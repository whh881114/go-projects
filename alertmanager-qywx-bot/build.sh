#!/bin/bash

registry_url="harbor.idc.roywong.work"
registry_project="library"
image="$registry_url"/"$registry_project"/alertmanager-qywx-bot:v1.02

docker build -t $image .
docker push $image