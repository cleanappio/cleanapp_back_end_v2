package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShutdownDrainsActiveRequestWithoutCancellingIt(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, finish, committed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
			t.Error("shutdown cancelled active request")
		case <-finish:
		}
		close(committed)
		_, _ = io.WriteString(w, "receipt committed")
	})}
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- serveUntilShutdown(ctx, srv, listener) }()
	response := make(chan string, 1)
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	go func() {
		r, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			response <- "request failed"
			return
		}
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		response <- string(raw)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	waitListenerClosed(t, listener.Addr().String())
	select {
	case err := <-ended:
		t.Fatalf("returned before transaction finished: %v", err)
	default:
	}
	close(finish)
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("drain did not finish")
	}
	select {
	case <-committed:
	default:
		t.Fatal("receipt work was abandoned")
	}
	if got := <-response; got != "receipt committed" {
		t.Fatal(got)
	}
}

func TestShutdownPropagatesListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	if err := serveUntilShutdown(context.Background(), &http.Server{}, listener); err == nil {
		t.Fatal("listener failure was reported as a graceful shutdown")
	}
}

func waitListenerClosed(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", address, 30*time.Millisecond)
		if err != nil {
			return
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatal("shutdown kept accepting requests")
		}
		time.Sleep(time.Millisecond)
	}
}

// This child runs only the HTTP shutdown helper: no production main, DB,
// Ethereum call, configuration loading, sender or migration is invoked.
func TestShutdownProcessChild(t *testing.T) {
	if os.Getenv("LEGACY_SHUTDOWN_CHILD") != "1" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	finish := make(chan struct{})
	go func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n'); close(finish) }()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("ACTIVE")
		<-finish
		if r.Context().Err() != nil {
			t.Error("active request cancelled")
		}
		_, _ = io.WriteString(w, "receipt committed")
	})}
	fmt.Printf("READY:%d:%s\n", os.Getpid(), listener.Addr())
	if err := serveUntilShutdown(ctx, srv, listener); err != nil {
		t.Fatal(err)
	}
}

func TestContainerCommandsExecAndDrainOnSignals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell and signals")
	}
	for _, service := range []struct{ directory, binary string }{{"docker_backend", "service"}, {"docker_pipelines", "pipelines"}} {
		for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
			t.Run(service.directory+"/"+sig.String(), func(t *testing.T) {
				raw, err := os.ReadFile(filepath.Join("..", "..", service.directory, "Dockerfile"))
				if err != nil {
					t.Fatal(err)
				}
				at := strings.Index(string(raw), "\nCMD ")
				if at < 0 {
					t.Fatal("runtime command missing")
				}
				command := strings.ReplaceAll(strings.TrimSpace(string(raw)[at+5:]), "\\\n", "")
				for _, forbidden := range []string{"--mysql_password", "--eth_private_key", "--eth_network_url", "MYSQL_APP_PASSWORD", "KITN_PRIVATE_KEY", "ETH_NETWORK_URL"} {
					if strings.Contains(command, forbidden) {
						t.Fatal("container command exposes secret configuration")
					}
				}
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				stub := filepath.Join(dir, "fixture")
				quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
				script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FIXTURE_ARGS\"\nexec " + quote(executable) + " -test.run '^TestShutdownProcessChild$'\n"
				if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
				command = strings.Replace(command, "\"./"+service.binary+"\"", quote(stub), 1)
				child := exec.Command("/bin/sh", "-c", command)
				child.Env = []string{"PATH=/usr/bin:/bin", "LEGACY_SHUTDOWN_CHILD=1", "FIXTURE_ARGS=" + filepath.Join(dir, "args"), "MYSQL_APP_PASSWORD=fixture-db", "PIPELINES_PORT=8090", "ETH_NETWORK_URL=fixture-network", "KITN_PRIVATE_KEY=fixture-key", "CONTRACT_ADDRESS=fixture-contract", "USERS_TABLE=fixture-users"}
				input, err := child.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				output, err := child.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := child.Start(); err != nil {
					t.Fatal(err)
				}
				waited := make(chan error, 1)
				go func() { waited <- child.Wait() }()
				t.Cleanup(func() { _ = child.Process.Kill() }) // Only the freshly owned fixture process.
				lines := make(chan string, 8)
				go func() {
					scanner := bufio.NewScanner(output)
					for scanner.Scan() {
						lines <- scanner.Text()
					}
					close(lines)
				}()
				next := func(prefix string) string {
					t.Helper()
					deadline := time.After(3 * time.Second)
					for {
						select {
						case line, ok := <-lines:
							if !ok {
								t.Fatal("child closed output")
							}
							if strings.HasPrefix(line, prefix) {
								return strings.TrimPrefix(line, prefix)
							}
						case <-deadline:
							t.Fatal("child marker timeout")
						}
					}
				}
				ready := next("READY:")
				parts := strings.SplitN(ready, ":", 2)
				if parts[0] != fmt.Sprint(child.Process.Pid) {
					t.Fatal("shell retained PID1 instead of exec")
				}
				address := parts[1]
				response := make(chan string, 1)
				client := &http.Client{Timeout: 3 * time.Second}
				defer client.CloseIdleConnections()
				go func() {
					r, err := client.Get("http://" + address)
					if err != nil {
						response <- "failed"
						return
					}
					defer r.Body.Close()
					b, _ := io.ReadAll(r.Body)
					response <- string(b)
				}()
				next("ACTIVE")
				if err := child.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				waitListenerClosed(t, address)
				select {
				case err := <-waited:
					t.Fatalf("child exited before active work completed: %v", err)
				default:
				}
				if _, err := io.WriteString(input, "finish transaction\n"); err != nil {
					t.Fatal(err)
				}
				if got := <-response; got != "receipt committed" {
					t.Fatal(got)
				}
				select {
				case err := <-waited:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("child failed to drain")
				}
				args, err := os.ReadFile(filepath.Join(dir, "args"))
				if err != nil {
					t.Fatal(err)
				}
				expected := "--mysql_host=cleanapp_db\n"
				if service.directory == "docker_pipelines" {
					expected = "--port=8090\n" + expected + "--contract_address=fixture-contract\n--users_table=fixture-users\n"
				}
				if string(args) != expected {
					t.Fatal("container argument expansion changed")
				}
			})
		}
	}
}
