#!/bin/bash
project=original_image
current_date=$(date +%y.%m.%d)
tag="v${current_date}"

docker build -t $project:$tag .
docker stop $project && docker rm $project
docker run --name $project -dp 8080:8080 -v $(pwd)/etc:/etc/app $project:$tag