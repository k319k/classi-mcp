import subprocess, sys, os

env = {'CLASSI_ID':'koiso0319','CLASSI_PASSWORD':'9gV1aY8t','CLASSI_ENABLE_POST':'false','CLASSI_ENABLE_STUDY':'true','CLASSI_ENABLE_CALENDAR':'true'}
proc = subprocess.Popen(['/root/classi-mcp/classi-mcp'],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
    env={**os.environ, **env}, bufsize=0)

# Write a newline immediately so Go binary starts reading
# (This ensures the binary is fully initialized)
proc.stdin.write(b'\n')
proc.stdin.flush()
# Read response (Go binary should ignore or return error for empty line)
try:
    proc.stdout.readline()
except:
    pass

# Now process Hermes messages
for line in sys.stdin:
    proc.stdin.write(line.encode())
    proc.stdin.flush()
    if 'notifications' in line:
        continue
    resp = proc.stdout.readline()
    if resp:
        sys.stdout.buffer.write(resp)
        sys.stdout.buffer.flush()
