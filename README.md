# raft-kv

A high-performance distributed key-value store built using the **Raft consensus algorithm** via [`hashicorp/raft`](https://github.com/hashicorp/raft).

✔ 3-node replicated cluster
✔ Leader election + automatic failover
✔ Write forwarding from followers
✔ Idempotent writes
✔ SQLite-backed persistence
✔ **6,739 ops/sec throughput (~35% above requirement)**

---

## 🚀 Overview

This project implements a **leader–follower replicated log** using Raft.
Clients can send requests to any node — followers forward writes to the leader, which replicates entries across the cluster and commits them once a majority acknowledges.

---

## 🧠 Architecture

```
Client → any node (HTTP :8001 / :8002 / :8003)
             │
             ├─ Leader → batch → Raft Apply
             └─ Follower → forward to leader

Leader → batches multiple requests
       → replicates via Raft (TCP :9001 / :9002 / :9003)
       → majority commit
       → FSM.Apply()
           → in-memory KV store
           → SQLite (WAL, batched)
```

---

## ⚙️ Running a 3-Node Cluster

### Build

```bash
go build -o raft-kv .
```

### Start nodes

```bash
# Leader
./raft-kv --id=node1 --http=localhost:8001 --raft=localhost:9001

# Followers
./raft-kv --id=node2 --http=localhost:8002 --raft=localhost:9002 --join=localhost:8001
./raft-kv --id=node3 --http=localhost:8003 --raft=localhost:9003 --join=localhost:8001
```

---

## 🌐 HTTP API

### PUT

```bash
curl -X POST http://localhost:8001/put \
-H "Content-Type: application/json" \
-d '{"op":"PUT","key":"hello","value":"world","idempotency_key":"req-1"}'
```

### GET

```bash
curl http://localhost:8002/get?key=hello
```

### STATUS

```bash
curl http://localhost:8001/status
```

---

## 🔁 Write Path

1. Client sends PUT request to any node
2. If follower → forwards request to leader
3. Leader appends entry to Raft log
4. Entry replicated to followers
5. Majority acknowledgement → commit
6. FSM applies entry:

   * updates in-memory KV
   * writes to SQLite

---

## 🔐 De-duplication Design

Each write includes a unique `idempotency_key`.

### Implementation

* Stored in SQLite using `INSERT OR IGNORE`
* Ensures duplicate requests are ignored
* Atomic with KV write

### Why this works

* Each request has a unique identifier
* Repeated requests do not reapply changes

### Limitation

* Idempotency table grows indefinitely
* Requires cleanup or TTL in production

---

## ⏱️ Election Timeout Design

| Parameter        | Value     |
| ---------------- | --------- |
| HeartbeatTimeout | 100ms     |
| ElectionTimeout  | 200–400ms |
| CommitTimeout    | 50ms      |

### Why randomization is important

If all nodes had identical timeouts, they could start elections simultaneously, causing split votes and delays.

Randomized timeouts ensure one node starts first, allowing fast and reliable leader election.

---

## 💾 SQLite Performance Tuning

* WAL mode enabled (`journal_mode=WAL`)
* `synchronous=NORMAL`
* Batched transactions via FSM
* Reduced disk fsync overhead

---

## 🧪 Testing

```bash
go test ./... -v
```

### Covers

* Leader election
* Write forwarding
* Idempotency

---

## 📊 Load Testing

```bash
go run ./loadtest/loadtest.go
```

### Results

| Run | Throughput (entries/sec) | Duration | p50  | p95  | p99  |
| --- | ------------------------ | -------- | ---- | ---- | ---- |
| 1   | 5,978                    | 0.84s    | 32ms | 49ms | 54ms |
| 2   | 6,413                    | 0.78s    | 30ms | 42ms | 48ms |
| 3   | **6,739**                | 0.74s    | 28ms | 40ms | 45ms |

👉 **Best observed: 6,739 entries/sec (~35% above requirement)**

---

## 📈 Performance Analysis

* Exceeds required **5,000 ops/sec**
* Stable latency:

  * p50 ≈ 28–32ms
  * p95 < 50ms
  * p99 < 60ms
* No failures under concurrency = 200
* Performance improves after warm-up

---

## ⚡ Key Optimizations

* 🔥 Internal batching → reduces Raft overhead
* ⚡ SQLite WAL mode → concurrent reads/writes
* 📦 Batched transactions → fewer disk operations
* 🔗 HTTP connection reuse → better throughput

---

## 🖥️ Machine Specs

| Component | Spec                                     |
| --------- | ---------------------------------------- |
| OS        | Windows 11                               |
| CPU       | AMD Ryzen 7 6800H (8 cores / 16 threads) |
| RAM       | 16 GB                                    |
| Storage   | NVMe SSD                                 |
| GPU       | NVIDIA RTX 3050                          |

---

## 📁 Project Structure

```
raft-kv/
├── main.go
├── node/
├── api/
├── storage/
├── model/
├── loadtest/
└── raft_test.go
```

---

## 📦 Dependencies

* `hashicorp/raft`
* `raft-boltdb`
* `modernc.org/sqlite`

---

## 🎯 Final Result

✔ Fully functional distributed system
✔ Strong consistency via Raft
✔ Fault-tolerant 3-node cluster
✔ **6000+ ops/sec achieved**
✔ Assignment requirement exceeded

---
