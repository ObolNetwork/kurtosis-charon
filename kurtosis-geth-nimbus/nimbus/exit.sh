#!/usr/bin/env bash

docker exec -it kurtosis-charon-vc$1-nimbus-1 /bin/bash -c "\
            mkdir /home/user/exits/
            cp -r /home/user/data/node$1/ /home/user/exits/
        
            /home/user/nimbus_beacon_node deposits exit --all --rest-url=http://node$1:3600/ --data-dir=/home/user/exits/node$1/"
