# Server Grouping Demo

This demonstrates the server grouping feature in Heimdall UI, which visually groups services by their host/node using bounding boxes.

## Setup

You'll need 3 terminal windows:

### Terminal 1: Start Heimdall

```bash
cd heimdall
./heimdall server -c heimdall.hcl
```

### Terminal 2: Start Server 1 Services

```bash
cd loki
go run ./cmd/loki server -c examples/server-1-services.hcl
```

This starts:
- `api-gateway` on port 8080
- `user-service` on port 8081

Both services will be grouped under **server-1** in the Heimdall UI.

### Terminal 3: Start Server 2 Services

```bash
cd loki
go run ./cmd/loki server -c examples/server-2-services.hcl
```

This starts:
- `product-service` on port 8083
- `analytics-service` on port 8084
- `order-service` on port 8082

All three services will be grouped under **server-2** in the Heimdall UI.

## View in Heimdall UI

Open http://localhost:9000 in your browser.

You should see:
- **Two bounding boxes** (dashed borders) labeled "server-1" and "server-2"
- **server-1** contains: api-gateway, user-service
- **server-2** contains: product-service, analytics-service, order-service
- **Edges** showing service dependencies (e.g., api-gateway → user-service, analytics-service → user-service)

## How It Works

1. Each Loki config specifies a `node_name` in the `heimdall` block:
   ```hcl
   heimdall {
     address   = "127.0.0.1:7946"
     node_name = "server-1"  # Custom node name for grouping
   }
   ```

2. When Loki joins the Heimdall mesh, it registers with this `node_name`

3. Heimdall API includes the `node_name` in the Service protobuf

4. The React UI groups services with the same `node_name` into visual bounding boxes

## Notes

- If `node_name` is not specified, it defaults to the machine's hostname
- Grouping only appears when there are multiple different node names or multiple services per node
- The bounding boxes are dashed to distinguish them from service nodes
- Each box shows a service count badge
