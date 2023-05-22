#!/bin/bash
project=original_image
current_date=$(date +%y%m%d)
tag="v${current_date}"

docker stop $project
docker rm $project

docker build -t $project:$tag .
docker run --name $project -dp 8000:8000 $project:$tag