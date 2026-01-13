#!/bin/bash

ARGS="--output /oplet/outputs/article.md"

case "${SEARCH_DEPTH}" in
  "2")
    ARGS="${ARGS} -d deep"
    ;;
  *)
    ARGS="${ARGS} -d basic"
    ;;
esac

ghostwriter write $ARGS