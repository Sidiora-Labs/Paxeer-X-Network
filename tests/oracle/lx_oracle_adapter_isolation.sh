#!/bin/sh
set -eu

for directory in modules ledger state protocol; do
    object_directory="build/obj/src/$directory"
    if [ ! -d "$object_directory" ]; then
        continue
    fi
    if find "$object_directory" -type f -name '*.o' -exec nm -u {} + |
        awk '{print $NF}' |
        grep -Eq '^(socket|connect|getaddrinfo|gethostbyname|send|recv|curl_easy_)'; then
        exit 1
    fi
done
