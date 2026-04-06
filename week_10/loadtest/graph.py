#!/usr/bin/env python3
"""
graph.py  —  Generate latency and read-write gap graphs from load test results.

Reads one or more newline-delimited JSON files produced by the Go load-test
client and generates the following PNG graphs:

  1. latency_cdf_<label>.png
        For each database configuration, CDF of read latency and write latency,
        with one curve per write-ratio.  X-axis is log-scaled to expose the
        long tail.  Dashed vertical lines mark p50/p95/p99.

  2. latency_hist_<label>.png
        For each configuration, a log-log histogram of latency for reads and
        writes separately.  The log-log scale makes heavy tails visually clear.

  3. latency_tail_<label>.png
        Tail zoom: CDF cropped to the p90–p100 region so outliers stand out.

  4. stale_reads.png
        Bar chart showing stale-read percentage per (config, write-ratio).

  5. rw_gap_distribution.png
        Histogram of the time gap between the last write to a key and a
        subsequent read of that same key.  Demonstrates the "local-in-time"
        property of the load generator.

Usage:
    python graph.py results/*.jsonl
    python graph.py --dir results/ --outdir graphs/
"""

import argparse
import json
import os
import sys
from collections import defaultdict
from pathlib import Path

import matplotlib
matplotlib.use("Agg")          # headless — no display required
import matplotlib.pyplot as plt
import matplotlib.ticker as ticker
import numpy as np


# ── colour palette ────────────────────────────────────────────────────────────

RATIO_META = {
    0.01: dict(color="#1f77b4", label="1% writes / 99% reads"),
    0.10: dict(color="#ff7f0e", label="10% writes / 90% reads"),
    0.50: dict(color="#2ca02c", label="50% writes / 50% reads"),
    0.90: dict(color="#d62728", label="90% writes / 10% reads"),
}

# Fallback for ratios not in the table above.
_FALLBACK_COLORS = ["#9467bd", "#8c564b", "#e377c2", "#7f7f7f"]


def ratio_meta(ratio):
    r = round(ratio, 2)
    if r in RATIO_META:
        return RATIO_META[r]
    idx = len(RATIO_META) % len(_FALLBACK_COLORS)
    return dict(color=_FALLBACK_COLORS[idx], label=f"{ratio*100:.0f}% writes")


# ── data loading ──────────────────────────────────────────────────────────────

def load_records(files):
    records = []
    for path in files:
        with open(path, encoding="utf-8") as f:
            for lineno, line in enumerate(f, 1):
                line = line.strip()
                if not line:
                    continue
                try:
                    records.append(json.loads(line))
                except json.JSONDecodeError as exc:
                    print(f"Warning: {path}:{lineno}: {exc}", file=sys.stderr)
    return records


# ── CDF helper ────────────────────────────────────────────────────────────────

def cdf(data):
    """Return (sorted_values, cumulative_percentiles) ready for plt.plot."""
    s = sorted(data)
    n = len(s)
    p = [100.0 * (i + 1) / n for i in range(n)]
    return s, p


# ── Graph 1: latency CDFs ─────────────────────────────────────────────────────

def _collect_latency(records):
    """Return grouped[label][ratio][type] = [latency_ms, ...] for successful ops."""
    grouped = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
    for r in records:
        sc = r.get("status_code", 0)
        if sc in (200, 201):
            grouped[r["label"]][round(r["write_ratio"], 2)][r["type"]].append(
                r["latency_ms"]
            )
    return grouped


def plot_latency_cdfs(records, outdir):
    """One PNG per label: read CDF (left) and write CDF (right), log x-scale."""

    grouped = _collect_latency(records)

    for label, ratio_data in sorted(grouped.items()):
        fig, axes = plt.subplots(1, 2, figsize=(14, 6))
        fig.suptitle(f"Latency CDF — {label}", fontsize=14, fontweight="bold")

        panels = [
            (axes[0], "read",  "Read Latency CDF"),
            (axes[1], "write", "Write Latency CDF"),
        ]

        for ax, rtype, title in panels:
            for ratio in sorted(ratio_data.keys()):
                lats = ratio_data[ratio].get(rtype, [])
                if not lats:
                    continue
                meta = ratio_meta(ratio)
                x, y = cdf(lats)
                ax.plot(x, y, color=meta["color"], label=meta["label"], linewidth=1.8)

                # Mark p50, p95, p99 with dashed verticals.
                for pct, ls in [(50, ":"), (95, "--"), (99, "-.")]:
                    v = np.percentile(lats, pct)
                    ax.axvline(v, color=meta["color"], linestyle=ls, alpha=0.35, linewidth=0.9)

            ax.set_xlabel("Latency (ms)", fontsize=11)
            ax.set_ylabel("Percentile (%)", fontsize=11)
            ax.set_title(title, fontsize=12)
            ax.set_ylim(0, 100)
            ax.set_xscale("log")                      # log-scale exposes the long tail
            ax.xaxis.set_major_formatter(ticker.ScalarFormatter())
            ax.legend(fontsize=9, loc="upper left")
            ax.grid(True, which="both", alpha=0.25)
            ax.set_yticks(range(0, 101, 10))

        fig.text(
            0.5, 0.01,
            "Dashed verticals: dotted=p50, dashed=p95, dash-dot=p99",
            ha="center", fontsize=8, color="gray",
        )

        path = os.path.join(outdir, f"latency_cdf_{label}.png")
        plt.tight_layout(rect=[0, 0.03, 1, 1])
        plt.savefig(path, dpi=150)
        plt.close()
        print(f"saved {path}")


def plot_latency_histograms(records, outdir):
    """
    Log-log histogram of latency per label.  Log x-axis = natural for latency;
    log y-axis makes the heavy tail visible even when the body dwarfs the tail.
    One PNG per label, read (left) and write (right).
    """
    grouped = _collect_latency(records)

    for label, ratio_data in sorted(grouped.items()):
        fig, axes = plt.subplots(1, 2, figsize=(14, 6))
        fig.suptitle(f"Latency Distribution (Histogram) — {label}",
                     fontsize=14, fontweight="bold")

        panels = [
            (axes[0], "read",  "Read Latency Distribution"),
            (axes[1], "write", "Write Latency Distribution"),
        ]

        for ax, rtype, title in panels:
            all_lats = [l for ratio in ratio_data.values()
                        for l in ratio.get(rtype, [])]
            if not all_lats:
                ax.set_title(title)
                continue

            # Common log-spaced bins across all ratios for comparability.
            lo = max(0.01, min(all_lats))
            hi = max(all_lats)
            bins = np.logspace(np.log10(lo), np.log10(hi + 1), 60)

            for ratio in sorted(ratio_data.keys()):
                lats = ratio_data[ratio].get(rtype, [])
                if not lats:
                    continue
                meta = ratio_meta(ratio)
                ax.hist(lats, bins=bins, alpha=0.55, color=meta["color"],
                        label=meta["label"], density=True)

                # Overlay p50 and p99 tick marks on x-axis.
                for pct, ls in [(50, "--"), (99, "-.")]:
                    v = np.percentile(lats, pct)
                    ax.axvline(v, color=meta["color"], linestyle=ls,
                               alpha=0.6, linewidth=1.0,
                               label=f"p{pct} ({meta['label'].split()[0]})" if ratio == sorted(ratio_data.keys())[0] else "")

            ax.set_xscale("log")
            ax.set_yscale("log")          # log y makes the tail visible
            ax.xaxis.set_major_formatter(ticker.ScalarFormatter())
            ax.set_xlabel("Latency (ms)", fontsize=11)
            ax.set_ylabel("Density (log scale)", fontsize=11)
            ax.set_title(title, fontsize=12)
            ax.legend(fontsize=8, loc="upper right")
            ax.grid(True, which="both", alpha=0.25)

        fig.text(
            0.5, 0.01,
            "Log-log scale: body of distribution on left, long tail visible on right",
            ha="center", fontsize=8, color="gray",
        )

        path = os.path.join(outdir, f"latency_hist_{label}.png")
        plt.tight_layout(rect=[0, 0.03, 1, 1])
        plt.savefig(path, dpi=150)
        plt.close()
        print(f"saved {path}")


def plot_latency_tail(records, outdir):
    """
    Tail zoom: CDF restricted to the p90–p100 region.
    Makes outlier structure in the far tail visually clear.
    One PNG per label.
    """
    grouped = _collect_latency(records)

    for label, ratio_data in sorted(grouped.items()):
        fig, axes = plt.subplots(1, 2, figsize=(14, 6))
        fig.suptitle(f"Tail Latency Zoom (p90–p100) — {label}",
                     fontsize=14, fontweight="bold")

        panels = [
            (axes[0], "read",  "Read Latency Tail"),
            (axes[1], "write", "Write Latency Tail"),
        ]

        for ax, rtype, title in panels:
            for ratio in sorted(ratio_data.keys()):
                lats = ratio_data[ratio].get(rtype, [])
                if len(lats) < 10:
                    continue
                meta = ratio_meta(ratio)
                x, y = cdf(lats)
                # Keep only the top 10 % of the CDF.
                cutoff = np.percentile(lats, 90)
                mask = [xi >= cutoff for xi in x]
                xs = [xi for xi, m in zip(x, mask) if m]
                ys = [yi for yi, m in zip(y, mask) if m]
                if xs:
                    ax.plot(xs, ys, color=meta["color"], label=meta["label"],
                            linewidth=1.8)
                    ax.axvline(np.percentile(lats, 99), color=meta["color"],
                               linestyle="-.", alpha=0.5, linewidth=1.0)

            ax.set_xlabel("Latency (ms)", fontsize=11)
            ax.set_ylabel("Percentile (%)", fontsize=11)
            ax.set_title(title, fontsize=12)
            ax.set_ylim(90, 100)
            ax.set_xscale("log")
            ax.xaxis.set_major_formatter(ticker.ScalarFormatter())
            ax.legend(fontsize=9, loc="upper left")
            ax.grid(True, which="both", alpha=0.25)

        fig.text(
            0.5, 0.01,
            "Dash-dot verticals mark p99 per curve",
            ha="center", fontsize=8, color="gray",
        )

        path = os.path.join(outdir, f"latency_tail_{label}.png")
        plt.tight_layout(rect=[0, 0.03, 1, 1])
        plt.savefig(path, dpi=150)
        plt.close()
        print(f"saved {path}")


# ── Graph 2: stale-read bar chart ─────────────────────────────────────────────

def plot_stale_reads(records, outdir):
    """Bar chart: stale-read % per (label, write_ratio)."""

    # stats[label][ratio] = {"reads": int, "stale": int}
    stats = defaultdict(lambda: defaultdict(lambda: {"reads": 0, "stale": 0}))
    for r in records:
        if r.get("type") != "read":
            continue
        label = r["label"]
        ratio = round(r["write_ratio"], 2)
        stats[label][ratio]["reads"] += 1
        if r.get("is_stale", False):
            stats[label][ratio]["stale"] += 1

    labels = sorted(stats.keys())
    ratios = sorted({ratio for d in stats.values() for ratio in d})
    if not labels or not ratios:
        return

    n_ratios = len(ratios)
    x = np.arange(len(labels))
    width = 0.8 / n_ratios

    fig, ax = plt.subplots(figsize=(max(8, 2 * len(labels)), 6))
    for i, ratio in enumerate(ratios):
        meta = ratio_meta(ratio)
        pcts = []
        for label in labels:
            d = stats[label].get(ratio, {"reads": 0, "stale": 0})
            pcts.append(d["stale"] / d["reads"] * 100 if d["reads"] > 0 else 0)
        offset = (i - n_ratios / 2 + 0.5) * width
        bars = ax.bar(x + offset, pcts, width * 0.9, label=meta["label"],
                      color=meta["color"], alpha=0.85)
        for bar, pct in zip(bars, pcts):
            if pct > 0.5:
                ax.text(
                    bar.get_x() + bar.get_width() / 2,
                    bar.get_height() + 0.3,
                    f"{pct:.1f}%", ha="center", va="bottom", fontsize=7,
                )

    ax.set_xlabel("Configuration", fontsize=12)
    ax.set_ylabel("Stale Read Rate (%)", fontsize=12)
    ax.set_title("Stale Read Rate by Configuration and Write Ratio", fontsize=13,
                 fontweight="bold")
    ax.set_xticks(x)
    ax.set_xticklabels(labels, fontsize=11)
    ax.legend(fontsize=9)
    ax.grid(True, axis="y", alpha=0.3)
    ax.set_ylim(0, max(1, ax.get_ylim()[1] * 1.15))

    path = os.path.join(outdir, "stale_reads.png")
    plt.tight_layout()
    plt.savefig(path, dpi=150)
    plt.close()
    print(f"saved {path}")


# ── Graph 3: read-write gap distribution ──────────────────────────────────────

def plot_rw_gap(records, outdir):
    """
    Histogram of the gap (ms) between the last write to a key and the
    subsequent read.  Shows how "local-in-time" the load generator is.
    One subplot per label.
    """

    # grouped[label][ratio] = [gap_ms, ...]
    grouped = defaultdict(lambda: defaultdict(list))
    for r in records:
        if r.get("type") == "read" and r.get("rw_gap_ms", -1) >= 0:
            grouped[r["label"]][round(r["write_ratio"], 2)].append(r["rw_gap_ms"])

    labels = sorted(grouped.keys())
    if not labels:
        print("No RW-gap data found; skipping gap plot.")
        return

    ncols = len(labels)
    fig, axes = plt.subplots(1, ncols, figsize=(6 * ncols, 5), squeeze=False)
    fig.suptitle(
        "Read-Write Gap Distribution\n"
        "(time from last confirmed write to the start of a read on the same key)",
        fontsize=12, fontweight="bold",
    )

    for col, label in enumerate(labels):
        ax = axes[0][col]
        all_gaps = [g for gaps in grouped[label].values() for g in gaps]
        if not all_gaps:
            ax.set_title(label)
            continue

        # Use a common bin edge set across ratios for comparability.
        bins = np.logspace(
            np.log10(max(0.1, min(all_gaps))),
            np.log10(max(all_gaps) + 1),
            50,
        )

        for ratio in sorted(grouped[label].keys()):
            gaps = grouped[label][ratio]
            if not gaps:
                continue
            meta = ratio_meta(ratio)
            ax.hist(gaps, bins=bins, alpha=0.55, color=meta["color"],
                    label=meta["label"], density=True)

        ax.set_xscale("log")
        ax.xaxis.set_major_formatter(ticker.ScalarFormatter())
        ax.set_xlabel("Gap (ms)", fontsize=11)
        ax.set_ylabel("Density", fontsize=11)
        ax.set_title(label, fontsize=12)
        ax.legend(fontsize=8)
        ax.grid(True, which="both", alpha=0.25)

    path = os.path.join(outdir, "rw_gap_distribution.png")
    plt.tight_layout()
    plt.savefig(path, dpi=150)
    plt.close()
    print(f"saved {path}")


# ── Summary table ─────────────────────────────────────────────────────────────

def print_summary(records):
    stats = defaultdict(lambda: defaultdict(lambda: {"writes": 0, "reads": 0, "stale": 0,
                                                      "read_lat": [], "write_lat": []}))
    for r in records:
        label = r.get("label", "?")
        ratio = round(r.get("write_ratio", 0), 2)
        t = r.get("type", "?")
        sc = r.get("status_code", 0)
        if t == "write":
            stats[label][ratio]["writes"] += 1
            if sc == 201:
                stats[label][ratio]["write_lat"].append(r["latency_ms"])
        elif t == "read":
            stats[label][ratio]["reads"] += 1
            if r.get("is_stale", False):
                stats[label][ratio]["stale"] += 1
            if sc == 200:
                stats[label][ratio]["read_lat"].append(r["latency_ms"])

    hdr = f"{'Config':<14} {'Ratio':>6}  {'Writes':>7} {'Reads':>7} {'Stale%':>7}  " \
          f"{'Read p50':>9} {'Read p99':>9}  {'Write p50':>10} {'Write p99':>10}"
    print()
    print(hdr)
    print("-" * len(hdr))
    for label in sorted(stats):
        for ratio in sorted(stats[label]):
            d = stats[label][ratio]
            stale_pct = d["stale"] / d["reads"] * 100 if d["reads"] else 0
            rp50 = np.percentile(d["read_lat"],  50) if d["read_lat"]  else float("nan")
            rp99 = np.percentile(d["read_lat"],  99) if d["read_lat"]  else float("nan")
            wp50 = np.percentile(d["write_lat"], 50) if d["write_lat"] else float("nan")
            wp99 = np.percentile(d["write_lat"], 99) if d["write_lat"] else float("nan")
            print(f"{label:<14} {ratio*100:>5.0f}%  {d['writes']:>7} {d['reads']:>7} "
                  f"{stale_pct:>6.2f}%  {rp50:>8.1f}ms {rp99:>8.1f}ms  "
                  f"{wp50:>9.1f}ms {wp99:>9.1f}ms")
    print()


# ── Entry point ───────────────────────────────────────────────────────────────

def main():
    ap = argparse.ArgumentParser(
        description="Generate load-test graphs from JSONL result files."
    )
    ap.add_argument("files", nargs="*", help="JSONL result files")
    ap.add_argument("--dir",    default=None, help="Directory containing JSONL files")
    ap.add_argument("--outdir", default=".",  help="Directory to save PNG files (default: .)")
    args = ap.parse_args()

    files = [Path(f) for f in args.files]
    if not files:
        search = Path(args.dir) if args.dir else Path(".")
        files = sorted(search.glob("*.jsonl"))
    if not files:
        print("No JSONL files found.", file=sys.stderr)
        sys.exit(1)

    print(f"Loading {len(files)} file(s): {[str(f) for f in files]}")
    records = load_records([str(f) for f in files])
    print(f"Loaded {len(records):,} records.")

    os.makedirs(args.outdir, exist_ok=True)
    print_summary(records)
    plot_latency_cdfs(records, args.outdir)
    plot_latency_histograms(records, args.outdir)
    plot_latency_tail(records, args.outdir)
    plot_stale_reads(records, args.outdir)
    plot_rw_gap(records, args.outdir)
    print("Done.")


if __name__ == "__main__":
    main()
