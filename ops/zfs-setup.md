# ZFS setup for marc

> Operator reference for one-time ZFS pool configuration and the weekly snapshot cron.
> Mirrors `docs/marc-spec-v1.md` §"Backup discipline (ZFS snapshots)".

## Why ZFS

MinIO retention is short (3 days for `raw/`, 7 days for `processed/raw-archive/`). That makes **ClickHouse the only long-term canonical record** of denoised conversations and Telegram answers. If ClickHouse loses data older than 7 days, it's gone.

ZFS gives us atomic point-in-time snapshots across ClickHouse and SQLite together, at near-zero per-snapshot disk cost (copy-on-write). That handles the most common failure modes — corruption, accidental table drop, bad migration — without any external backup infrastructure.

What ZFS does **not** give us: disk-failure recovery (snapshots live on the same disk as live data) or off-machine recovery (an unbootable Ubuntu host loses access to its snapshots). Both are explicit v1 trade-offs and are deferred to v2.

## One-time setup

Run these once when the host is first provisioned. All commands are root.

### 1. Install ZFS userspace tools

```bash
sudo apt install zfsutils-linux
```

(The kernel module ships with stock Ubuntu 24.04 — no DKMS step needed.)

### 2. Create the pool on a dedicated disk

Use `/dev/disk/by-id/...` rather than `/dev/sdX` so the pool survives reboots and disk-letter reordering.

```bash
ls /dev/disk/by-id/   # find the right disk
sudo zpool create -o ashift=12 marc-data /dev/disk/by-id/<your-disk-id>
```

`ashift=12` aligns to 4 KB physical sectors — correct for any modern SSD or HDD. Setting it wrong at create time is unfixable without recreating the pool.

### 3. Create datasets

One dataset per logical store, so snapshots are granular and rollbacks don't disturb unrelated data:

```bash
sudo zfs create marc-data/clickhouse
sudo zfs create marc-data/state
```

### 4. Enable LZ4 compression

LZ4 is fast (CPU-bounded, near-zero latency overhead) and helps significantly with ClickHouse's text columns:

```bash
sudo zfs set compression=lz4 marc-data
```

### 5. Cap the ARC

ZFS's adaptive replacement cache competes with ClickHouse's own page cache for memory. Cap ARC to a sensible fraction of system RAM. For a 32 GB host dedicated to marc, 8 GB is a reasonable starting point:

```bash
sudo tee /etc/modprobe.d/zfs.conf >/dev/null <<'EOF'
options zfs zfs_arc_max=8589934592
EOF
```

The value is bytes (8 × 1024³ = 8589934592). Reboot or `sudo modprobe -r zfs && sudo modprobe zfs` to pick up.

### 6. Repoint mount points

Stop the affected services before moving data. ClickHouse data lives at `/var/lib/clickhouse`; marc's SQLite state lives at `/var/lib/marc/state`. The exact one-time migration:

```bash
sudo systemctl stop clickhouse-server marc-process.service marc-generate.timer 2>/dev/null

# ClickHouse
sudo zfs set mountpoint=/var/lib/clickhouse marc-data/clickhouse
sudo rsync -a /var/lib/clickhouse.old/ /var/lib/clickhouse/   # if you have existing data
sudo chown -R clickhouse:clickhouse /var/lib/clickhouse

# marc state
sudo zfs set mountpoint=/var/lib/marc/state marc-data/state
sudo rsync -a /var/lib/marc/state.old/ /var/lib/marc/state/   # if you have existing data
sudo chown -R <marc-user>:<marc-group> /var/lib/marc/state

sudo systemctl start clickhouse-server marc-process.service
```

On a fresh host with no existing data the rsync steps are unnecessary.

## Weekly snapshot cron

Install the bundled cron file:

```bash
sudo cp ops/marc-snapshots.cron /etc/cron.d/marc-snapshots
sudo chmod 0644 /etc/cron.d/marc-snapshots
```

The schedule is **Sunday 03:00 local time**, recursive across `marc-data/*` so ClickHouse + SQLite snapshots are consistent with each other. Snapshot names embed the date (`weekly-YYYYMMDD`) and sort lexicographically. **No automatic pruning** — at marc's scale, weekly deltas accumulate to ~10 GB/year, which is cheaper than building and validating a pruner.

Verify after the first run:

```bash
sudo zfs list -t snapshot
# NAME                                     USED  AVAIL  REFER  MOUNTPOINT
# marc-data/clickhouse@weekly-20260503     12K   -      4.2G   -
# marc-data/state@weekly-20260503          8K    -      36K    -
```

## Recovery scenarios

### Corrupted SQLite state

```bash
sudo systemctl stop marc-process.service marc-generate.timer
sudo zfs rollback marc-data/state@weekly-<YYYYMMDD>
sudo systemctl start marc-process.service marc-generate.timer
```

`zfs rollback` discards every snapshot newer than the target. To preserve newer snapshots, clone instead:

```bash
sudo zfs clone marc-data/state@weekly-<YYYYMMDD> marc-data/state-recovery
# inspect/extract from /var/lib/marc/state-recovery/, then destroy the clone
```

### ClickHouse data corruption

Same pattern, but always stop ClickHouse first — rolling back a live data dir is undefined behaviour:

```bash
sudo systemctl stop clickhouse-server marc-process.service
sudo zfs rollback marc-data/clickhouse@weekly-<YYYYMMDD>
sudo systemctl start clickhouse-server marc-process.service
```

### Accidental table drop

Don't roll back the whole dataset — clone the snapshot, extract the missing table, copy rows into the live dataset:

```bash
sudo zfs clone marc-data/clickhouse@weekly-<YYYYMMDD> marc-data/ch-recovery
# Point a temporary clickhouse-server at /marc-data/ch-recovery on a different
# port, dump the dropped table with clickhouse-client INSERT INTO ... FROM s2,
# then destroy the clone.
sudo zfs destroy marc-data/ch-recovery
```

### Disk failure

**Not recoverable in v1.** Snapshots live on the same physical disk as live data, so a disk that fails takes both with it. The system is rebuilt from scratch; capture begins fresh from each client's first new ANTHROPIC_BASE_URL request.

Off-machine snapshot replication (e.g. `zfs send | ssh remote zfs receive`) is the v2 mitigation and is explicitly deferred.

## Operational notes

- **Adding a dataset**: `sudo zfs create marc-data/<new-dataset>` is enough — the recursive cron will pick it up automatically next Sunday.
- **Discovering snapshot drift**: `sudo zfs list -t snapshot -o name,used,creation marc-data` shows every snapshot ever taken with its delta size. A sudden jump usually means schema migration or large `OPTIMIZE TABLE` ran since the prior snapshot.
- **Manual snapshot before risky changes**: `sudo zfs snapshot -r marc-data@premigrate-$(date +%Y%m%d-%H%M)` — ad hoc snapshots coexist peacefully with the weekly schedule.
- **Free-space monitoring**: `sudo zpool list marc-data` shows usage. ZFS performance degrades sharply above 80 % full; plan to grow or prune before then.
