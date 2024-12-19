#!/bin/bash

PORTS=("14000" "24000" "34000" "14003" "24003" "34003" "14004" "24004" "34004")

for PORT in "${PORTS[@]}"; do
    PID=$(sudo lsof -ti :$PORT)
    if [ ! -z "$PID" ]; then
        echo "Killing process $PID on port $PORT"
        sudo kill -9 $PID
    else
        echo "No process found on port $PORT"
    fi
done
