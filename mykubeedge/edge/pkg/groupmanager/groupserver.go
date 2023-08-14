package groupmanager

import (
	"context"
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

func (gm *groupManager) startGroupServer(stopChan <-chan struct{}) {
	http.HandleFunc("/nodeRole", gm.nodeRoleHandler)
	http.HandleFunc("/RequestVote", gm.voteHandler)
	http.HandleFunc("/IAmLeader", gm.leaderHandler)
	http.HandleFunc("/LeaderIP", gm.leaderIPHandler)
	srv := &http.Server{Addr: "0.0.0.0:10551"}
	go func() {
		<-stopChan
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			klog.Errorf("Server shutdown failed: %s", err)
		}

	}()
	klog.Info("group Server start at :10551")
	klog.Error(srv.ListenAndServe())
}

func (gm *groupManager) nodeRoleHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(gm.nodeRole))
}

func (gm *groupManager) voteHandler(w http.ResponseWriter, r *http.Request) {
	if !gm.hasVoted && gm.nodeRole == Follower {
		gm.hasVoted = true
		klog.Infof("agree a vote of %s", r.RemoteAddr)
		w.Write([]byte("VoteForYou"))
	} else {
		klog.Infof("reject a vote of %s", r.Host)
		w.Write([]byte("RejectVote"))
	}
}

func (gm *groupManager) leaderHandler(w http.ResponseWriter, r *http.Request) {
	klog.Infof("get leader broadcast from %s", r.RemoteAddr)
	gm.lock.Lock()
	gm.nodeRole = Follower
	gm.leaderIP = strings.Split(r.RemoteAddr, ":")[0]
	gm.lock.Unlock()
	w.Write([]byte("OK"))
}

func (gm *groupManager) leaderIPHandler(w http.ResponseWriter, r *http.Request) {
	// klog.Infof("receive a leader ip request from %s", r.RemoteAddr)
	gm.lock.RLock()
	leaderIP := gm.leaderIP
	gm.lock.RLocker().Unlock()
	w.Write([]byte(leaderIP))
}
