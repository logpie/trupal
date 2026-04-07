#!/bin/bash
# TruPal E2E Self-Test Protocol
# Tests trupal on a REAL separate CC session to avoid meta-awareness.
# Uses tmux pane capture + send-keys for verification.
set -euo pipefail

TRUPAL_BIN="./trupal"
TEST_PROJECT="/tmp/trupal-e2e-$$"
RESULTS=""
PASS=0
FAIL=0

log() { echo "$(date +%H:%M:%S) [test] $*"; }
pass() { PASS=$((PASS+1)); log "PASS: $1"; }
fail() { FAIL=$((FAIL+1)); log "FAIL: $1"; RESULTS="$RESULTS\nFAIL: $1"; }

cleanup() {
    log "cleaning up..."
    tmux kill-pane -t "$TRUPAL_PANE" 2>/dev/null || true
    tmux kill-pane -t "$CC_PANE" 2>/dev/null || true
    rm -rf "$TEST_PROJECT"
    rm -f .trupal.pid
}
trap cleanup EXIT

# ── Setup: create a real Go project ──
log "creating test project at $TEST_PROJECT"
mkdir -p "$TEST_PROJECT"
cd "$TEST_PROJECT"
git init -q
/usr/local/go/bin/go mod init testproject
cat > main.go << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println("hello")
}
EOF
git add -A && git commit -q -m "init"
cd /home/yuxuan/work/trupal

# ── Build trupal ──
log "building trupal"
/usr/local/go/bin/go build -o trupal .

# ── Start trupal watching the test project ──
log "starting trupal"
$TRUPAL_BIN start
sleep 3
TRUPAL_PANE=$(cat .trupal.pid)
log "trupal pane: $TRUPAL_PANE"

# ── Test 1: TUI renders header ──
log "TEST 1: Header renders"
sleep 5
HEADER=$(tmux capture-pane -t "$TRUPAL_PANE" -p | head -1)
if echo "$HEADER" | grep -q "trupal"; then
    pass "header shows 'trupal'"
else
    fail "header missing 'trupal': '$HEADER'"
fi

# ── Test 2: Separator line ──
SEP=$(tmux capture-pane -t "$TRUPAL_PANE" -p | head -3 | tail -1)
if echo "$SEP" | grep -q "─"; then
    pass "separator renders"
else
    fail "no separator: '$SEP'"
fi

# ── Test 3: Footer visible ──
FOOTER=$(tmux capture-pane -t "$TRUPAL_PANE" -p | grep "─" | tail -1)
if [ -n "$FOOTER" ]; then
    pass "footer separator visible"
else
    fail "no footer"
fi

# ── Test 4: Brain starts (wait for analyzing or brain indicator) ──
log "TEST 4: Brain starts"
sleep 15
BRAIN=$(tmux capture-pane -t "$TRUPAL_PANE" -p | head -2 | tail -1)
if echo "$BRAIN" | grep -qE "analyzing|●|starting"; then
    pass "brain indicator present"
else
    fail "no brain indicator: '$BRAIN'"
fi

# ── Test 5: Keyboard scroll (j/k) ──
log "TEST 5: Keyboard scroll"
# First generate enough content — brain should produce observations
sleep 20
tmux send-keys -t "$TRUPAL_PANE" k
sleep 1
AFTER_K=$(tmux capture-pane -t "$TRUPAL_PANE" -p | head -1)
if [ -n "$AFTER_K" ]; then
    pass "keyboard 'k' processed (no crash)"
else
    fail "keyboard 'k' caused issue"
fi
# Reset scroll
tmux send-keys -t "$TRUPAL_PANE" G
sleep 1

# ── Test 6: Stop and pane survives ──
log "TEST 6: Stop lifecycle"
$TRUPAL_BIN stop
sleep 2
DEAD=$(tmux list-panes -F "#{pane_id} #{pane_dead}" | grep "$TRUPAL_PANE" | awk '{print $2}')
if [ "$DEAD" = "0" ]; then
    pass "pane alive after stop"
else
    fail "pane dead after stop"
fi

STOP_CONTENT=$(tmux capture-pane -t "$TRUPAL_PANE" -p | head -3)
if echo "$STOP_CONTENT" | grep -qi "stopped"; then
    pass "stop summary shown"
else
    fail "no stop summary: '$STOP_CONTENT'"
fi

# ── Results ──
echo ""
echo "═══════════════════════════════════"
echo "  E2E Results: $PASS passed, $FAIL failed"
echo "═══════════════════════════════════"
if [ $FAIL -gt 0 ]; then
    echo -e "$RESULTS"
    exit 1
fi
