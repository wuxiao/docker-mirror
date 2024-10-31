#!/bin/sh

go build -o docker-mirror main.go
sudo chmod +x docker-mirror
cp docker-mirror ~/Libs/bin/