# vhs tape — TokenSched 10-second happy path.
# Render in CI via charmbracelet/vhs-action (see .github/workflows/demo.yml),
# or locally: `vhs docs/demo.tape` (needs the `tokensched` binary on PATH).
Output assets/demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1100
Set Height 640
Set Theme "Dracula"
Set Padding 18

Type "# 1) See the task tree and per-node budget"
Enter
Sleep 600ms
Type "tokensched plan examples/overrun-tasktree.yaml --budget 200k"
Enter
Sleep 2.5s

Type "# 2) Schedule it — down-tier + preempt to fit the window"
Enter
Sleep 600ms
Type "tokensched schedule examples/overrun-tasktree.yaml --budget 200k"
Enter
Sleep 3s
