#!/bin/bash

make agentimage
docker rmi $(docker images | grep "none" | awk '{print $3}')
containerID=$(docker ps|grep \"edgemesh-agent\" |awk '{split($1, arr, "\t"); print arr[1]}')
docker stop $containerID
docker rm -f $containerID
