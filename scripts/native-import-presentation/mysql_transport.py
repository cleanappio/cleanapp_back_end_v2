"""Trusted host-local MySQL CLI transport; credentials stay in DB containers.

No connection is made at import time. SQL errors expose only a numeric error
code; raw SQL, report contents, credentials and driver diagnostics are withheld.
"""
import decimal
import os
import re
import selectors
import subprocess
import tempfile
import time
import uuid

TARGETS = {
    "production": ("cleanapp_db", "cleanapp"),
    "fixture": ("ca-olm-import-test-mysql", "cleanapp_olm_import_test"),
}


class MysqlError(RuntimeError):
    def __init__(self, code=None, *, timeout=False):
        self.code = code
        self.timeout = timeout
        self.retryable = timeout or code in (1205, 1213, 3024)
        super().__init__("MySQL query timed out; connection closed" if timeout else
            "MySQL command failed (error code " + (str(code) if code is not None else "unavailable") + ")")


def sql_string(value):
    if value is None:
        return "NULL"
    if not isinstance(value, str):
        raise TypeError("SQL text must be a string or None")
    encoded = value.encode("utf-8", "strict")
    if len(encoded) > 16 * 1024**2:
        raise ValueError("SQL text exceeds reviewed bound")
    # An introduced HEX string is a literal (coercibility4); a CONVERT()
    # expression can conflict with deployed unicode_ci/0900 column collations.
    return "_utf8mb4 X'" + encoded.hex() + "'"


def sql_bytes(value):
    if not isinstance(value, (bytes, bytearray, memoryview)):
        raise TypeError("SQL BLOB must be bytes")
    if len(value) > 16 * 1024**2:
        raise ValueError("SQL BLOB exceeds reviewed bound")
    return "X'" + bytes(value).hex() + "'"


def sql_num(value):
    if isinstance(value, bool) or value is None:
        raise TypeError("SQL number must be finite numeric data")
    try:
        number = decimal.Decimal(str(value))
    except (decimal.InvalidOperation, ValueError):
        raise ValueError("Invalid SQL number") from None
    if not number.is_finite() or abs(number.adjusted()) > 128 or len(number.as_tuple().digits) > 128:
        raise ValueError("SQL number outside reviewed finite bounds")
    result = format(number, "f")
    if len(result) > 512:
        raise ValueError("SQL number exceeds reviewed length")
    return result


class Mysql:
    def __init__(self, target="production", readonly=False):
        if target not in TARGETS or type(readonly) is not bool:
            raise ValueError("Unexpected database target or read-only mode")
        container, database = TARGETS[target]
        self.target = target
        self.readonly = readonly
        self._closed = False
        self.buffer = b""
        self.stderr = tempfile.TemporaryFile("w+b")
        # The fixed shell command expands only the existing container variable.
        # Neither the controller nor the command arguments contain its value.
        fixed = ('MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot --batch --raw '
                 '--skip-column-names --unbuffered --binary-mode '
                 '--default-character-set=utf8mb4 --max-allowed-packet=64M ' + database)
        try:
            self.process = subprocess.Popen(["sudo", "-n", "docker", "exec", "-i", container, "sh", "-c", fixed],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.stderr)
            os.set_blocking(self.process.stdin.fileno(), False)
            self.selector = selectors.DefaultSelector()
            self.selector.register(self.process.stdout, selectors.EVENT_READ)
            self.query("SET SESSION time_zone='+00:00'; "
                "SET SESSION sql_mode='STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'; "
                "SET SESSION lock_wait_timeout=2; SET SESSION innodb_lock_wait_timeout=2; "
                "SET SESSION max_execution_time=5000;")
            if readonly:
                # Session default covers even a caller's subsequent explicit
                # START TRANSACTION, so plan mode cannot accidentally write.
                self.query("SET SESSION TRANSACTION READ ONLY;")
        except Exception:
            self.close()
            raise

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()

    def _error(self):
        self.stderr.flush()
        self.stderr.seek(0)
        raw = self.stderr.read()
        matches = re.findall(rb"\bERROR\s+(\d{3,5})\b", raw)
        return MysqlError(int(matches[-1]) if matches else None)

    def query(self, sql, timeout=30):
        if self._closed:
            raise MysqlError()
        if not isinstance(sql, str) or not sql.strip():
            raise TypeError("SQL must be nonempty text")
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or not 0 < timeout <= 120:
            raise ValueError("Query timeout outside reviewed bound")
        token = "OLM_QUERY_END_" + uuid.uuid4().hex
        payload = (sql.rstrip().rstrip(";") + ";\nSELECT '" + token + "';\n").encode("utf-8")
        if len(payload) > 32 * 1024**2:
            raise ValueError("SQL packet exceeds reviewed bound")
        deadline = time.monotonic() + timeout
        written = 0
        self.selector.register(self.process.stdin, selectors.EVENT_WRITE)
        lines = []
        result_bytes = 0
        while True:
            while b"\n" in self.buffer:
                raw, self.buffer = self.buffer.split(b"\n", 1)
                try:
                    line = raw.decode("utf-8", "strict").rstrip("\r")
                except UnicodeDecodeError:
                    self.close()
                    raise MysqlError() from None
                if line == token:
                    return lines
                result_bytes += len(raw) + 1
                if result_bytes > 64 * 1024**2 or len(lines) >= 100000:
                    self.close()
                    raise ValueError("SQL result exceeds reviewed transport bound")
                lines.append(line)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                self.close()
                raise MysqlError(timeout=True)
            for key, _ in self.selector.select(remaining):
                if key.fileobj is self.process.stdin:
                    try:
                        written += os.write(self.process.stdin.fileno(), payload[written:written + 65536])
                    except BlockingIOError:
                        continue
                    except (BrokenPipeError, OSError):
                        error = self._error()
                        self.close()
                        raise error from None
                    if written == len(payload):
                        self.selector.unregister(self.process.stdin)
                else:
                    chunk = os.read(self.process.stdout.fileno(), 65536)
                    if not chunk:
                        error = self._error()
                        self.close()
                        raise error
                    self.buffer += chunk
                    if len(self.buffer) > 64 * 1024**2 or len(lines) > 100000:
                        self.close()
                        raise ValueError("SQL result exceeds reviewed transport bound")

    def scalar(self, sql):
        rows = self.query(sql)
        if len(rows) != 1 or "\t" in rows[0]:
            raise ValueError("Expected one MySQL scalar")
        return rows[0]

    def close(self):
        if self._closed:
            return
        self._closed = True
        process = getattr(self, "process", None)
        if process is not None and process.poll() is None:
            try:
                process.stdin.close()
                process.wait(timeout=3)
            except (OSError, subprocess.TimeoutExpired):
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=3)
        selector = getattr(self, "selector", None)
        if selector is not None:
            selector.close()
        if process is not None:
            for stream in (process.stdin, process.stdout):
                if stream is not None:
                    stream.close()
        self.stderr.close()
