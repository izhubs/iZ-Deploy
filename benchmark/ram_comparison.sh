#!/bin/bash
# Script to measure RAM usage of Docker daemon and deployment agents

echo "--- RAM Comparison Benchmark ---"
echo "Collecting RSS memory for agents..."

# izDeploy
IZDEPLOY_PID=$(pgrep -f izdeploy-agent | head -n 1)
if [ -n "$IZDEPLOY_PID" ]; then
    IZDEPLOY_RAM=$(ps -o rss= -p $IZDEPLOY_PID | awk '{print $1/1024 " MB"}')
    echo "izDeploy (PID: $IZDEPLOY_PID): $IZDEPLOY_RAM"
else
    echo "izDeploy: Not running"
fi

# Coolify
COOLIFY_PID=$(pgrep -f coolify | head -n 1)
if [ -n "$COOLIFY_PID" ]; then
    COOLIFY_RAM=$(ps -o rss= -p $COOLIFY_PID | awk '{print $1/1024 " MB"}')
    echo "Coolify (PID: $COOLIFY_PID): $COOLIFY_RAM"
else
    echo "Coolify: Not running"
fi

# Dokploy
DOKPLOY_PID=$(pgrep -f dokploy | head -n 1)
if [ -n "$DOKPLOY_PID" ]; then
    DOKPLOY_RAM=$(ps -o rss= -p $DOKPLOY_PID | awk '{print $1/1024 " MB"}')
    echo "Dokploy (PID: $DOKPLOY_PID): $DOKPLOY_RAM"
else
    echo "Dokploy: Not running"
fi

# Docker Daemon
DOCKER_PID=$(pgrep -f dockerd | head -n 1)
if [ -n "$DOCKER_PID" ]; then
    DOCKER_RAM=$(ps -o rss= -p $DOCKER_PID | awk '{print $1/1024 " MB"}')
    echo "Docker Daemon (PID: $DOCKER_PID): $DOCKER_RAM"
else
    echo "Docker Daemon: Not running"
fi

echo "--- Done ---"
