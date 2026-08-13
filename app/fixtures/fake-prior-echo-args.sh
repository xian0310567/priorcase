#!/bin/sh
# 받은 인자를 한 줄에 하나씩 낸다. 인자가 쪼개지거나 합쳐지면 여기서 드러난다.
for a in "$@"; do printf '%s\n' "$a"; done
