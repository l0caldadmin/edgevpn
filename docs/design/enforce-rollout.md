# M2: Enforce Rollout — Operator Cutover Guide

> Milestone 2 transitions the ownership system from **observe mode** (M1) to **enforce mode**, 
> where the system actively rejects unauthorized mutations (hijacks, replays, version rollbacks).

**Status**: Production-ready for M2 cutover after M1 stabilization period.  
**Duration**: Typically 1-2 weeks of enforce mode operation before moving to M3.

## Overview

Enforce mode extends observe mode with strict validation:

| Aspect | Observe (M1) | Enforce (M2) |
|--------|--------------|-------------|
| **Signatures** | Verified but logged | Verified and enforced |
| **Hijacks** | Detected, allowed to propagate | Rejected, entry preserved |
| **Replays** | Detected, allowed to propagate | Rejected, old version ignored |
| **Version drift** | Tolerated | Strictly monotonic |
| **Tombstones** | Validated, no ownership check | Ownership-validated |

## Prerequisites

- [ ] All nodes running M1 (observe mode) for minimum 7 days
- [ ] Ledger has stabilized (no repeated hijack attempts)
- [ ] Health check TTL is uniform across cluster
- [ ] All nodes have recent heartbeats
- [ ] Backup of ledger data from stable M1 period

## Upgrade Path

### 1. Soft Cutover (Day 0)

**Goal**: Enable enforcement without crashing non-compliant entries.

```bash
# On all nodes, update config:
export EDGEVPNOWNERSHIP=enforce

# Restart services (rolling restart, 1 node at a time):
systemctl restart edgevpn
systemctl restart edgevpn-dns
systemctl restart edgevpn-relay
```

**Expected behavior during soft cutover:**

- New mutations must be valid (signed, no hijacks)
- Existing observations remain queryable
- Non-compliant entries are not deleted; they're guarded against override
- Logs show `hijack detected` or `replay detected` for rejected mutations

### 2. Monitor (Days 1-3)

**Watch these metrics:**

```bash
# Check for rejection patterns:
tail -f /var/log/edgevpn.log | grep "hijack\|replay\|signature"

# Verify ownership resolution:
edgevpn-cli peers list --show-owner

# Check health status:
curl http://localhost:7861/api/v1/health | jq '.ownership_mode'
```

**Expected**: Very few rejections (only stale or crafted mutations).

### 3. Stabilization (Days 4-7)

**Run acceptance tests:**

```bash
# Test hijack rejection:
go test -vet=off ./pkg/blockchain -run TestEnforceMode

# Verify no regressions:
go test -vet=off ./pkg/vpn ./pkg/config ./pkg/services ./pkg/blockchain

# Smoke test DNS and relay:
nslookup testnode.mesh localhost
curl http://egress-node.mesh:3000/test
```

### 4. Validation (Days 8-14)

**Confirm enforcement is working:**

```bash
# Check ledger stats:
edgevpn-cli stats ledger --mode enforce

# Verify reaper is running (orphaned entries cleanup):
edgevpn-cli stats ledger --show-reaped

# Measure mutation acceptance rate:
# (Valid mutations / total mutations) > 0.99
```

## Rollback Plan

If enforce mode causes instability:

### Immediate Rollback (< 15 min)

```bash
# On all nodes:
export EDGEVPNOWNERSHIP=observe

# Rolling restart:
systemctl restart edgevpn

# Verify reversion:
curl http://localhost:7861/api/v1/health | jq '.ownership_mode'
```

**No data loss**: Observe mode is fully backward-compatible with enforce mode ledger state.

### Extended Fallback (if needed)

If observe mode also shows issues, fall back to `off` mode:

```bash
export EDGEVPNOWNERSHIP=off
systemctl restart edgevpn
```

This preserves all ledger data but disables ownership checks entirely.

## Key Differences from M1

### Strict Mutation Validation

**M1 (Observe):**
```
Entry arrives with signature and version
→ Validate signature (log if invalid)
→ Accept unconditionally
```

**M2 (Enforce):**
```
Entry arrives with signature and version
→ Validate signature (reject if invalid)
→ Check for hijacks (reject if live owner != claimant)
→ Check for replays (reject if version <= current)
→ Check for tombstone forgery (reject if non-owner tombstones live entry)
→ Accept only if all checks pass
```

### Replay Detection

**Enabled in M2:**

- Block-level: same block version resubmitted → rejected
- Entry-level: `version <= current_version` → rejected
- Example: `A signs v5 → system has v5 → A resubmits v3 → rejected`

### Hijack Protection

**Enabled in M2:**

- If entry owner is live, only that owner can mutate it
- Non-owner updates are rejected, even if higher version
- Example: `A owns IP x → B claims IP x (B's version > A's) → rejected if A is live`

### Version Monotonicity

- Blocks must have strictly increasing version numbers
- Entries must have versions >= current version (for same owner)
- No drift tolerance (drift = 0)

## Monitoring and Alerting

### Key Metrics to Watch

```yaml
# Rejection rate (should be < 1%)
rejection_rate = rejected_mutations / total_mutations

# Hijack attempts (should trend to 0)
hijack_attempts = counter_increase(rejected_hijacks, 1m)

# Replay attempts (should trend to 0)
replay_attempts = counter_increase(rejected_replays, 1m)

# Signature failures (should be 0)
signature_failures = counter_increase(rejected_signatures, 1m)

# Orphaned entries (should trend to 0 as reaper runs)
orphaned_entries = ledger_entries{status="orphaned"}
```

### Alert Conditions

| Condition | Severity | Action |
|-----------|----------|--------|
| `rejection_rate > 5%` | Warning | Investigate mutation patterns; may indicate misconfiguration |
| `hijack_attempts > 100/hour` | Critical | Possible coordinated attack; consider rolling back |
| `signature_failures > 0` | Critical | Key distribution issue; investigate crypto setup |
| `reaper_stalls > 3` | Warning | Reaper leader election may be failing |

## Troubleshooting

### "hijack detected" Errors

**Cause:** Non-owner trying to update a live owner's entry.  
**Resolution:**
- Check if the non-owner node is configured correctly
- Verify owner's health check is still active
- If owner is dead, wait for reaper to mark entry for reclaim (TTL + 1 min)

### "replay detected" Errors

**Cause:** Old version of an entry is being resubmitted.  
**Resolution:**
- Check if gossip is duplicating old blocks
- Verify block version is monotonically increasing
- Look for clock skew between nodes (use `ntpstat`)

### "signature verification failed" Errors

**Cause:** Entry signature is invalid or corrupted.  
**Resolution:**
- Check libp2p key setup
- Verify peer IDs match between nodes
- Ensure no middleware is stripping signatures

### High Reaper CPU Usage

**Cause:** Ledger has many orphaned entries and reaper is processing them.  
**Resolution:**
- This is expected during transition; reaper runs once per health check cycle
- Monitor `orphaned_entries` metric; should decrease over time
- If stuck, manually trigger reaper: `edgevpn-cli admin reap`

## Operational Runbook

### Emergency Enforcement Disable

If enforce mode causes a production outage:

```bash
# On each node in sequence (rolling):
ssh node1 "export EDGEVPNOWNERSHIP=observe && systemctl restart edgevpn"
ssh node2 "export EDGEVPNOWNERSHIP=observe && systemctl restart edgevpn"
# ... repeat for all nodes

# Verify cluster health:
edgevpn-cli status --show-ownership-mode
```

### Re-enable After Fix

Once the issue is identified and fixed:

```bash
# Roll forward one node at a time:
ssh node1 "export EDGEVPNOWNERSHIP=enforce && systemctl restart edgevpn"

# Monitor for 15 minutes:
ssh node1 "tail -f /var/log/edgevpn.log | grep -E 'hijack|replay|signature'"

# If stable, continue to next node:
ssh node2 "export EDGEVPNOWNERSHIP=enforce && systemctl restart edgevpn"
```

## Post-Cutover Validation

After stable M2 operation (7-14 days):

- [ ] Mutation acceptance rate > 99%
- [ ] Zero signature verification failures
- [ ] Hijack attempts ≤ 5 (residual)
- [ ] No version rollback errors
- [ ] Reaper successfully cleaning orphaned entries
- [ ] Operators confident with rejection logs
- [ ] Ready for M3 (experimental stabilization)

## M2 → M3 Transition

Once M2 is stable, begin M3 stabilization phase:
- Add flag controls for enforcement mode granularity
- Test mixed-version clusters (M2 + M3 nodes)
- Prepare M3 documentation

## References

- **Design Doc**: `docs/design/authenticated-ledger.md` (Section 6: Enforcement)
- **M1 Runbook**: `docs/design/ownership-rollout.md#m1-observe-rollout`
- **Test Suite**: `pkg/blockchain/enforce_test.go`
- **CLI**: `edgevpn-cli ownership-mode --help`

---

**Last Updated**: 2026-07-05  
**Milestone**: M2 (Enforce Rollout)  
**Status**: Ready for production deployment
