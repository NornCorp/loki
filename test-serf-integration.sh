#!/bin/bash
set -e

echo "Building binaries..."
cd /Users/erik/code/instruqt/norncorp

# Build heimdall
cd heimdall
go build -o /tmp/heimdall ./cmd/heimdall
cd ..

# Build loki
cd loki
go build -o /tmp/loki ./cmd/loki
cd ..

echo "Starting Heimdall..."
/tmp/heimdall server -c heimdall/examples/heimdall.hcl &
HEIMDALL_PID=$!

# Give Heimdall time to start
sleep 2

echo "Starting Loki with Heimdall integration..."
/tmp/loki server -c loki/examples/with-heimdall.hcl &
LOKI_PID=$!

# Give Loki time to join the mesh
sleep 3

echo "Testing Loki service..."
curl -s http://localhost:8080/hello | grep "Hello from Loki" && echo "✓ Loki service responding"

echo "Cleaning up..."
kill $LOKI_PID 2>/dev/null || true
kill $HEIMDALL_PID 2>/dev/null || true

# Give processes time to clean up
sleep 2

echo "✓ Integration test completed successfully"
