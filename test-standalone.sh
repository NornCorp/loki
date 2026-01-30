#!/bin/bash
set -e

echo "Building Loki..."
cd /Users/erik/code/instruqt/norncorp/loki
go build -o /tmp/loki ./cmd/loki

echo "Starting Loki in standalone mode (without Heimdall)..."
/tmp/loki server -c examples/hello.hcl &
LOKI_PID=$!

# Give Loki time to start
sleep 2

echo "Testing Loki service..."
curl -s http://localhost:8080/hello | grep "Hello from Loki" && echo "✓ Loki service responding"
curl -s http://localhost:8080/health | grep "healthy" && echo "✓ Health check responding"

echo "Cleaning up..."
kill $LOKI_PID 2>/dev/null || true

# Give process time to clean up
sleep 1

echo "✓ Standalone test completed successfully"
