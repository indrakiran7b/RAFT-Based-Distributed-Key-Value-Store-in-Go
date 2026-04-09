package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"raft-kv/api"
	"raft-kv/node"
	"raft-kv/storage"
)

func main() {
	id := flag.String("id", "node1", "node id")
	httpAddr := flag.String("http", "localhost:8001", "http listen address")
	raftAddr := flag.String("raft", "localhost:9001", "raft listen address")
	joinAddr := flag.String("join", "", "http address of any cluster node to join (empty = bootstrap)")
	flag.Parse()

	log.Println("Starting node:", *id)

	db := storage.InitDB(*id + ".db")
	fsm := node.NewFSM(db)

	// Bootstrap only if no join address provided
	bootstrap := *joinAddr == ""
	n := node.NewNode(*id, "./data/"+*id, *raftAddr, bootstrap, fsm)

	// Register HTTP handlers
	srv := &api.Server{Node: n}
	http.HandleFunc("/put", srv.Put)
	http.HandleFunc("/get", srv.Get)
	http.HandleFunc("/status", srv.Status)
	http.HandleFunc("/join", srv.Join)

	// Start HTTP server in background
	go func() {
		log.Println("HTTP server running at", *httpAddr)
		log.Fatal(http.ListenAndServe(*httpAddr, nil))
	}()

	// If a join address was given, contact it to join the cluster
	if *joinAddr != "" {
		// Wait a moment for our own HTTP + Raft transport to be ready
		time.Sleep(1 * time.Second)

		joinReq, _ := json.Marshal(map[string]string{
			"id":   *id,
			"addr": *raftAddr,
		})

		// Retry joining for up to 15 seconds (leader may not be elected yet)
		joined := false
		for i := 0; i < 15; i++ {
			resp, err := http.Post(
				"http://"+*joinAddr+"/join",
				"application/json",
				bytes.NewBuffer(joinReq),
			)
			if err == nil && resp.StatusCode == 200 {
				log.Println("Successfully joined cluster via", *joinAddr)
				joined = true
				break
			}
			log.Println("Join attempt", i+1, "failed, retrying...")
			time.Sleep(1 * time.Second)
		}
		if !joined {
			log.Fatal("Could not join cluster after retries")
		}
	}

	// Block forever
	select {}
}
