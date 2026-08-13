#!/bin/sh
# 1MB 를 낸다. 파이프 버퍼(보통 64KB)보다 훨씬 크다 —
# 읽는 쪽이 파이프를 안 비우면 여기서 블록된다.
i=0
while [ $i -lt 1024 ]; do
  printf '%01024d' $i
  i=$((i+1))
done
