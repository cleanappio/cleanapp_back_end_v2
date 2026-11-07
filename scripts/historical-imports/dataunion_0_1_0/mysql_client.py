"""Trusted local-DB mysql client without moving production credentials.
Only run on the trusted AWS production/import host. SQL should use hex literals.
"""
import os,pathlib,selectors,subprocess,tempfile,time,uuid

class Mysql:
    def __init__(self,container='cleanapp_db',database='cleanapp',readonly=False):
        if (container,database) not in {('cleanapp_db','cleanapp'),('ca-import-test-mysql','cleanapp_dataunion_import_test')}:raise ValueError('Unexpected DB target')
        self.stderr=tempfile.TemporaryFile('w+b');self.buffer=b''
        self.process=subprocess.Popen(['sudo','docker','exec','-i',container,'sh','-c',
            'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot --batch --raw --skip-column-names --unbuffered --binary-mode --max-allowed-packet=64M '+database],
            stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=self.stderr)
        self.selector=selectors.DefaultSelector();self.selector.register(self.process.stdout,selectors.EVENT_READ)
        self.query("SET SESSION time_zone='+00:00'; SET SESSION sql_mode='STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'; SET SESSION innodb_lock_wait_timeout=5; SET SESSION max_execution_time=30000;")
        if readonly:self.query('START TRANSACTION READ ONLY;')
    def __enter__(self):return self
    def __exit__(self,*args):self.close()
    def errors(self):
        self.stderr.flush();self.stderr.seek(0);return self.stderr.read().decode('utf-8','replace')[-8000:]
    def query(self,sql,timeout=60):
        if not isinstance(sql,str):raise TypeError('SQL must be text')
        token='IMPORT_END_'+uuid.uuid4().hex
        payload=(sql.rstrip().rstrip(';')+";\nSELECT '"+token+"';\n").encode('utf-8')
        try:self.process.stdin.write(payload);self.process.stdin.flush()
        except (BrokenPipeError,OSError) as e:raise RuntimeError('mysql client exited: '+self.errors()) from e
        deadline=time.monotonic()+timeout;lines=[]
        while True:
            while b'\n' in self.buffer:
                raw,self.buffer=self.buffer.split(b'\n',1);line=raw.decode('utf-8','strict').rstrip('\r')
                if line==token:return lines
                lines.append(line)
            remaining=deadline-time.monotonic()
            if remaining<=0:raise TimeoutError('mysql response timeout; connection must be closed to roll back')
            if not self.selector.select(remaining):continue
            chunk=os.read(self.process.stdout.fileno(),65536)
            if not chunk:raise RuntimeError('mysql client exited: '+self.errors())
            self.buffer+=chunk
    def scalar(self,sql):
        rows=self.query(sql)
        if len(rows)!=1 or '\t' in rows[0]:raise ValueError('Expected one SQL scalar')
        return rows[0]
    def close(self):
        if self.process.poll() is None:
            try:self.process.stdin.close();self.process.wait(timeout=5)
            except (OSError,subprocess.TimeoutExpired):self.process.terminate();self.process.wait(timeout=5)
        self.selector.close();self.stderr.close()
