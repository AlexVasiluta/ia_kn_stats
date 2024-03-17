#!/bin/bash
# Used by me, stripped out private data

cd "$(dirname "$0")"

go build -v . || exit 0

./ia_kn_stats -export_days=30 \
    -export_months=12 -export_roll_months=3 -export_roll_days=30 \
    -export_path="./kn_ia_stats.body" \
    -kilonova_dsn="postgres://..." | tee logs/logfile_$(date '+%Y-%m-%d-%H').txt
