package main

import (
	"math/rand"
	"os"
	"time"

	"k8s.io/component-base/logs"

	"github.com/kubeedge/edgemesh/cmd/edgemesh-gateway/app"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	command := app.NewEdgeMeshGatewayCommand()

	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
