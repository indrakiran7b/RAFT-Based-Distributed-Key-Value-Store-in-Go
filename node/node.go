package node

import (
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

type Node struct {
	Raft *raft.Raft
	FSM  *FSM
}

func NewNode(id, dir, addr string, bootstrap bool, fsm *FSM) *Node {
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(id)

	// Heartbeat: how often the leader sends heartbeats to followers.
	config.HeartbeatTimeout = 100 * time.Millisecond

	// Election timeout: how long a follower waits with no heartbeat before
	// starting an election. Randomized between 200-400ms so that if multiple
	// followers time out simultaneously (e.g. after a leader crash), they
	// don't all campaign at once -- staggering means one node typically wins
	// before others even start, avoiding split-vote loops where no candidate
	// reaches majority and the cluster stalls leaderless.
	config.ElectionTimeout = time.Duration(200+rand.Intn(200)) * time.Millisecond

	// LeaderLeaseTimeout must be strictly less than HeartbeatTimeout.
	config.LeaderLeaseTimeout = 80 * time.Millisecond

	// CommitTimeout: max wait before force-flushing a batch to followers.
	// 5ms (tighter than the spec's 50ms) improves throughput under high
	// concurrency by shipping accumulated log entries sooner per round-trip.
	config.CommitTimeout = 5 * time.Millisecond
	config.MaxAppendEntries = 256

	// Snapshot every 500 committed entries, checked every 10s.
	// After each snapshot Raft truncates the log behind it, keeping the
	// BoltDB log file small and preventing slowdown across repeated runs.
	config.SnapshotThreshold = 500
	config.SnapshotInterval = 10 * time.Second

	os.MkdirAll(dir, 0700)

	logStore, err := raftboltdb.NewBoltStore(dir + "/log.db")
	if err != nil {
		log.Fatal(err)
	}

	stableStore, err := raftboltdb.NewBoltStore(dir + "/stable.db")
	if err != nil {
		log.Fatal(err)
	}

	snapStore, err := raft.NewFileSnapshotStore(dir, 1, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}

	transport, err := raft.NewTCPTransport(addr, nil, 3, 10*time.Second, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}

	r, err := raft.NewRaft(config, fsm, logStore, stableStore, snapStore, transport)
	if err != nil {
		log.Fatal(err)
	}

	hasState, err := raft.HasExistingState(logStore, stableStore, snapStore)
	if err != nil {
		log.Fatal(err)
	}

	if bootstrap && !hasState {
		log.Println("Bootstrapping single-node cluster:", id)
		cfg := raft.Configuration{
			Servers: []raft.Server{
				{ID: raft.ServerID(id), Address: raft.ServerAddress(addr)},
			},
		}
		f := r.BootstrapCluster(cfg)
		if err := f.Error(); err != nil {
			log.Fatal("Bootstrap failed:", err)
		}
	}

	log.Println("Raft node started:", id)
	return &Node{Raft: r, FSM: fsm}
}

func (n *Node) Join(id, addr string) error {
	f := n.Raft.AddVoter(raft.ServerID(id), raft.ServerAddress(addr), 0, 10*time.Second)
	return f.Error()
}
