# The README's demo: review a branch, an agent addresses the thread, resolve it.
# mise run demo renders it to docs/demo.gif, on the repository demo/fixture.sh makes in $GR_DEMO.

Output docs/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1400
Set Height 900
# Wide enough for the captions and overlays to sit outside the terminal, clear of gr's bars.
Set Padding 64
Set TypingSpeed 40ms
Set Theme "Builtin Dark"
Set CaptionKeyStyle "vim"
Set CaptionAlignment "bottom-right"
Set CaptionFontSize 20
Set CaptionBoxColor "#3a3a3a"
Set CaptionBoxOpacity 0.9
Set CaptionBoxPadding 6
Set CaptionMarginVertical 8
Set CaptionMarginRight 64
Set CaptionInactivityTimer 1500ms
Set OverlayAlignment "top-left"
Set OverlayFontSize 20
Set OverlayBoxColor "#3a3a3a"
Set OverlayBoxOpacity 0.9
Set OverlayBoxPadding 6
Set OverlayMarginVertical 8
Set OverlayMarginLeft 64

Hide
Type `. "$GR_DEMO/env.sh" && cd "$GR_DEMO/repo" && clear`
Enter
Show

# Review in the TUI.
Overlay@3s "Review a branch in the TUI"
Type "gr"
Enter
Sleep 2s
CaptionOn
Type "}"
Sleep 1s
Type "l"
Sleep 300ms
Type@80ms "jjjjjjjjjjj"
Sleep 500ms
Type "c"
Sleep 500ms
CaptionOff
Type "This raises ZeroDivisionError for an empty list."
Sleep 500ms
CaptionOn
Ctrl+S
Sleep 1s
Type "S"
Sleep 1s
Type "r"
Sleep 500ms
CaptionOff
Type "Handle an empty list."
Sleep 500ms
CaptionOn
Ctrl+S
Sleep 2s
Type "q"
CaptionOff
Sleep 500ms

# An agent addresses the thread from the CLI.
Hide
Type "fix && clear"
Enter
Sleep 500ms
Show
Overlay@3s "An agent reads and addresses it"
Type "gr threads --state open"
Enter
Sleep 3s
Type `GR_AUTHOR=Claude gr address 1 -m "Fixed in a360752: mean returns 0.0 for an empty list."`
Sleep 500ms
Enter
Sleep 2s

# Back in the TUI, see the fix and resolve the thread.
Overlay@3s "Check the fix, then resolve"
Type "gr"
Enter
Sleep 3s
CaptionOn
Type "}"
Sleep 1s
Type "}"
Sleep 3s
Type "{"
Sleep 300ms
Type "{"
Sleep 1s
Type "t"
Sleep 1s
Type "x"
Sleep 3s
