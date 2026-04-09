# raft-kv

A distributed key-value store built on the Raft consensus protocol using
[`hashicorp/raft`](https://github.com/hashicorp/raft). A 3-node cluster
elects a leader automatically, accepts writes on any node (followers
forward to the leader), replicates entries across all nodes, and
persists committed state to SQLite.

------------------------------------------------------------------------

## Architecture

    Client → any node (HTTP :8001 / :8002 / :8003)
                 │
                 ├─ if leader  → enqueue → batch → Raft Apply
                 └─ if follower → forward to leader

    Leader → batches multiple requests
           → replicates via Raft (TCP :9001 / :9002 / :9003)
           → majority commit
           → FSM.Apply()
               → in-memory KV
               → SQLite (WAL, batched)

------------------------------------------------------------------------

## Running a 3-Node Cluster Locally

``` bash
go build -o raft-kv .
./raft-kv --id=node1 --http=localhost:8001 --raft=localhost:9001
./raft-kv --id=node2 --http=localhost:8002 --raft=localhost:9002 --join=localhost:8001
./raft-kv --id=node3 --http=localhost:8003 --raft=localhost:9003 --join=localhost:8001
```

------------------------------------------------------------------------

## HTTP API

``` bash
curl -X POST http://localhost:8001/put -H "Content-Type: application/json" -d '{"op":"PUT","key":"hello","value":"world","idempotency_key":"req-1"}'
curl http://localhost:8002/get?key=hello
```

------------------------------------------------------------------------

## Load Test

``` bash
go run ./loadtest/loadtest.go
```

### Results

  Run   Throughput (entries/sec)   Duration   p50    p95    p99
  ----- -------------------------- ---------- ------ ------ ------
  1     5,978                      0.84s      32ms   49ms   54ms
  2     6,413                      0.78s      30ms   42ms   48ms
  3     **6,739**                  0.74s      28ms   40ms   45ms

**Best observed: 6,739 entries/sec (\~35% above requirement)**

------------------------------------------------------------------------

## Performance Analysis

-   Exceeds 5,000 entries/sec
-   Stable latency (p95 \< 50ms)
-   No failures under concurrency=200

------------------------------------------------------------------------

## Machine Specs

  Component   Spec
  ----------- -------------------
  OS          Windows 11
  CPU         AMD Ryzen 7 6800H
  RAM         16 GB
  Storage     NVMe SSD
  GPU         RTX 3050

------------------------------------------------------------------------

## Project Structure

    raft-kv/
    ├── main.go
    ├── node/
    ├── api/
    ├── storage/
    ├── model/
    ├── loadtest/
    └── raft_test.go
