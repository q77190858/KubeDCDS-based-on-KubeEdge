#!/bin/bash
cp _output/local/bin/edgecore ~/edgecore-1.12.2
for i in {101..140}  
do
sshpass -p "5210" scp ~/edgecore-1.12.2 root@192.168.0.${i}:~
echo send 192.168.0.${i}
done

# sshpass -p "5210" scp ~/edgecore-1.12.2 root@192.168.0.101:~
# sshpass -p "5210" scp ~/edgecore-1.12.2 root@192.168.0.102:~
