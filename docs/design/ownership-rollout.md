# Ledger Ownership Enforcement Rollout

Status: **In Progress** · Phase: **Observe** · Target: Safe network-wide adoption with visibility before enforcement.

## Overview

This document covers the phased rollout strategy for authenticated ledger entries and ownership enforcement. The goal is to move networks from legacy (unsigned, trust-any-peer) to ownership-enforced (signed, owner-gated) in a controlled, observable way.

## Rollout Phases

### Phase 1: Observe Mode (Current)
- **Mode flag**: `--ownership=observe` or `EDGEVPNOWNERSHIP=observe`
- **Behavior**: All writes are signed and versioned. Unauthorized writes are logged as warnings but accepted.
- **Operators see**: Exact count, frequency, and source of policy violations without network disruption.
- **How long**: Until warning volume stabilizes to zero or negligible (typically 1–2 weeks on a live network).
- **Exit criteria**:
  - No warning flood in steady state.
  - All nodes on network can handle signed entries (backward-compatible receive).
  - Operators comfortable with violation patterns (or violations are due to expected misconfiguration).

### Phase 2: Enforce Mode
- **Mode flag**: `--ownership=enforce` or `EDGEVPNOWNERSHIP=enforce` (binary default)
- **Behavior**: Unauthorized writes are rejected; policy violations are logged as warnings (one per rejected write).
- **Network requirement**: All nodes must be in observe or enforce. Mixing with off-mode breaks the ledger.
- **How to deploy**:
  1. Upgrade all nodes to the new binary.
  2. All nodes start in enforce by default.
  3. If violations are detected by operations, revert to observe temporarily on the whole network.
  4. Fix root cause (e.g., misbehaving peer, misconfiguration) and retry.
- **Exit criteria**: No rejections, network operates as expected, data converges cleanly.

### Phase 3: Off Mode (Legacy Opt-Out)
- **Mode flag**: `--ownership=off` or `EDGEVPNOWNERSHIP=off`
- **Behavior**: Entries are not signed; ledger behavior reverts to height-wins, legacy semantics.
- **When to use**: Only for networks that cannot upgrade together, or for library users who want legacy behavior.
- **Note**: Not recommended for production; security guarantees are lost.

## Operator Runbook

### Pre-Rollout Checklist
- [ ] All nodes can be upgraded simultaneously (maintenance window scheduled).
- [ ] Backups of ledger state are available.
- [ ] Monitoring/logging for ledger conflicts is in place.
- [ ] Team is familiar with rollback procedure (see below).

### Rollout Steps: Observe → Enforce

1. **Snapshot current state** (optional but recommended):
   - Dump ledger state from one node for comparison post-upgrade.
   ```bash
   curl http://localhost:8080/api/ledger > ledger-pre-upgrade.json
   ```

2. **Upgrade all nodes to new binary**:
   - Binary defaults to `--ownership=enforce`.
   - Operators can override with `--ownership=observe` to delay enforce.
   ```bash
   # Option A: Use observe mode for this deploy
   export EDGEVPNOWNERSHIP=observe
   edgevpn --config config.yaml
   
   # Option B: Use enforce (default)
   edgevpn --config config.yaml  # defaults to --ownership=enforce
   ```

3. **Monitor warnings** (if in observe mode):
   - Tail logs for "ownership violation" entries.
   - Track source peer IDs and buckets.
   ```bash
   tail -f /var/log/edgevpn.log | grep -i "ownership\|unauthorized"
   ```

4. **Investigate violations**:
   - If a peer is trying to overwrite another's entry, identify why.
   - Common causes:
     - Duplicate peer ID (two nodes with same identity, likely clock skew or key reuse).
     - Stale cached entry on a client trying to re-announce.
     - Misconfigured peer allowed to write to protected buckets.

5. **Resolve and test**:
   - Fix root cause.
   - If needed, restart the offending peer in observe mode to stabilize.
   - Wait for warning volume to drop.

6. **Move to enforce** (once observe is clean):
   - Either restart nodes with `EDGEVPNOWNERSHIP=enforce` or let them default.
   - Network will reject unauthorized writes.
   - Verify ledger converges without errors.

### Rollback Procedure

If enforce mode causes issues:

1. **Switch all nodes back to observe**:
   ```bash
   export EDGEVPNOWNERSHIP=observe
   edgevpn --config config.yaml
   ```

2. **Wait for ledger to stabilize** (1–2 minutes).

3. **Identify root cause** before attempting enforce again.

4. **Revert to off mode only as last resort**:
   ```bash
   export EDGEVPNOWNERSHIP=off
   edgevpn --config config.yaml
   ```

## Configuration Reference

| Env Var | Flag | Default | Effect |
|---------|------|---------|--------|
| `EDGEVPNOWNERSHIP` | `--ownership` | `enforce` | Ownership mode: off, observe, or enforce |
| `EDGEVPNOWNERSHIPTTL` | `--ownership-ttl` | 120s (2m) | Liveness window for inactive owner cleanup |

## Expected Behavior by Mode

| Behavior | Off | Observe | Enforce |
|----------|-----|---------|---------|
| Entries signed | ✗ | ✓ | ✓ |
| Unauthorized write rejected | ✗ | ✗ | ✓ |
| Violation warning logged | ✗ | ✓ | ✗* |
| Replay attacks prevented | ✗ | ✓ | ✓ |
| Inactive entries reaped | ✗ | ✓ | ✓ |

*Enforce mode currently logs a warning per rejected write ("ownership violation (rejected): ..."); consider log filtering/rate limiting if volume is high.

## Library Usage (Embedders)

If you embed EdgeVPN as a library (e.g., LocalAI, Kairos):

- Default: `--ownership=off` (library users opt in explicitly).
- To enable observe: `node.WithOwnership(config.ObserveMode)`.
- To enable enforce: `node.WithOwnership(config.EnforceMode)`.
- Ensure your embedder can restart nodes in observe mode if issues arise during enforce.

## Monitoring and Alerting

### Key Metrics to Track

1. **Violation count** (observe mode):
   - Alert if > 10 violations/minute (suggests systemic misconfiguration).

2. **Ledger merge latency** (enforce mode):
   - Track time between incoming block and merge completion.
   - Alert if > 5s (suggests merge stall or rejection surge).

3. **Reaper cadence** (both modes):
   - Verify reaper runs every scrub interval (default ~5 min).
   - Count of tombstones created and pruned.

4. **Ledger size** (both modes):
   - Track byte size of ledger state.
   - Alert if > 2x baseline (suggests entries not expiring).

### Log Pattern Matching

**Observe mode warnings**:
```
ownership violation: unauthorized write to bucket=machines key=peer-X-ip from source=peer-Y
```

**Enforce mode rejections** (sparse, aggregated):
```
ledger merge: dropped N unauthorized writes; reaper reaped M stale entries
```

## Troubleshooting

### Issue: High violation rate in observe mode

**Symptoms**: 100s of violations/minute, mostly to `machines` or `services` bucket.

**Diagnosis**:
1. Check for duplicate peer IDs: `curl http://localhost:8080/api/machines | jq -r '[] | .peer_id' | sort | uniq -d`
2. Check node clock synchronization: `timedatectl status` on each peer.

**Fix**:
- Regenerate identity on duplicate nodes (delete privkey cache, restart).
- Sync clocks (NTP).

### Issue: Ledger does not converge in enforce mode

**Symptoms**: Network partitions, entries missing, DNS not resolving.

**Diagnosis**:
1. Check logs for "dropped unauthorized" entries.
2. Identify which peer is being rejected.
3. Check if that peer's heartbeat is stale: `curl http://localhost:8080/api/health | jq '.peer_X'`

**Fix**:
- If peer heartbeat is stale, it's being reaped; the peer will reclaim its entries once it re-announces.
- If peer is live but writes are rejected, the peer may have incorrect private key (check identity rotation logs).

### Issue: Rollback from enforce to observe causes ledger divergence

**Symptoms**: Entries differ across peers after switching modes.

**Diagnosis**: Off → Observe transition is not atomic; some peers see observe blocks before others.

**Workaround**:
- Perform rollback only when network is quiet (no active VPN traffic).
- Restart all nodes within 30 seconds of each other to ensure unified state.

## Next Steps

1. Deploy binary with `EDGEVPNOWNERSHIP=observe` (recommended initial rollout).
2. Monitor for 1 week.
3. Schedule enforce rollout once violations stabilize.
4. After enforce is stable, schedule `--ownership-ttl` tuning if needed (default is 2 minutes; production may use 5–10 minutes).
