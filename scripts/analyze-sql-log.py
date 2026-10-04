#!/usr/bin/env python3
"""Stream mirror-server JSON logs and summarize SQL cost without changing logs."""
import argparse
import gzip
import heapq
import json
from collections import Counter
from datetime import datetime, timedelta, timezone
from pathlib import Path


def analyze(directory, day, limit=20):
    zone = timezone(timedelta(hours=8))
    files = sorted(p for p in Path(directory).rglob(f"*{day}*.log*")
                   if p.name.endswith((".log", ".log.gz")))
    groups, seconds, slowest, messages = {}, Counter(), [], Counter()
    unreadable, invalid, matched, serial = [], 0, 0, 0
    for path in files:
        try:
            opener = gzip.open(path, "rt", errors="replace") if path.suffix == ".gz" else path.open(errors="replace")
            with opener as stream:
                for line in stream:
                    try:
                        row = json.loads(line)
                        stamp = datetime.fromisoformat(row["time"].replace("Z", "+00:00")).astimezone(zone)
                    except (ValueError, KeyError, TypeError):
                        invalid += 1
                        continue
                    if stamp.strftime("%Y-%m-%d") != day or row.get("component") != "master":
                        continue
                    matched += 1
                    message = row.get("msg", "")
                    if message != "SQL 耗时":
                        if row.get("level") in ("WARN", "ERROR"):
                            messages[(row.get("level"), message, row.get("type", ""))] += 1
                        continue
                    duration = row.get("duration", 0)
                    if not isinstance(duration, (int, float)) or duration < 0:
                        invalid += 1
                        continue
                    key = (row.get("kind", ""), row.get("sql", ""))
                    stat = groups.setdefault(key, {"count": 0, "total_ns": 0, "max_ns": 0,
                                                   "ge_100ms": 0, "max_at": ""})
                    stat["count"] += 1
                    stat["total_ns"] += duration
                    stat["ge_100ms"] += duration >= 100_000_000
                    if duration > stat["max_ns"]:
                        stat["max_ns"], stat["max_at"] = duration, row["time"]
                    # Prepare calls are reported separately and are not executions.
                    if key[0] != "prepare":
                        seconds[stamp.strftime("%H:%M:%S")] += 1
                        serial += 1
                        sample = {k: row[k] for k in ("time", "kind", "sql", "status", "duration", "error") if k in row}
                        heapq.heappush(slowest, (duration, serial, sample))
                        if len(slowest) > limit:
                            heapq.heappop(slowest)
        except (OSError, EOFError) as error:
            unreadable.append({"file": str(path), "error": str(error)})
    rows = [{"kind": kind, "sql": sql, "count": stat["count"],
             "total_ms": round(stat["total_ns"] / 1e6, 3),
             "avg_ms": round(stat["total_ns"] / stat["count"] / 1e6, 3),
             "max_ms": round(stat["max_ns"] / 1e6, 3),
             "ge_100ms": stat["ge_100ms"], "max_at": stat["max_at"]}
            for (kind, sql), stat in groups.items()]
    executions = [row for row in rows if row["kind"] != "prepare"]
    return {
        "summary": {"day": day, "timezone": "+08:00", "files": [str(p) for p in files],
                    "matched_logs": matched, "sql_executions": sum(seconds.values()),
                    "invalid_lines": invalid, "unreadable_files": unreadable,
                    "note": "Grouped by logged SQL text (may be truncated); driver durations exclude pool wait and log writing. Totals are summed durations, not wall-clock busy time."},
        "highest_frequency": sorted(executions, key=lambda r: r["count"], reverse=True)[:limit],
        "highest_total_cost": sorted(executions, key=lambda r: r["total_ms"], reverse=True)[:limit],
        "highest_single_cost": sorted(executions, key=lambda r: r["max_ms"], reverse=True)[:limit],
        "prepare_frequency": sorted((r for r in rows if r["kind"] == "prepare"), key=lambda r: r["count"], reverse=True)[:10],
        "slowest_samples": [sample for _, _, sample in sorted(slowest, reverse=True)],
        "peak_seconds": seconds.most_common(10),
        "warnings": [{"level": key[0], "message": key[1], "type": key[2], "count": count}
                     for key, count in messages.most_common(20)],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory")
    parser.add_argument("--day", required=True)
    args = parser.parse_args()
    datetime.strptime(args.day, "%Y-%m-%d")
    report = analyze(args.directory, args.day)
    for section, value in report.items():
        print(f"\n=== {section} ===")
        for row in value if isinstance(value, list) else [value]:
            print(json.dumps(row, ensure_ascii=False))


if __name__ == "__main__":
    main()
