# Re-record: KUBECONFIG=<demo kubeconfig with context "staging"> docs/demo/setup.sh && vhs docs/demo/demo.tape
Output docs/demo.gif

Set Shell zsh
Set FontSize 19
Set Width 1240
Set Height 720
Set Padding 26
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set TypingSpeed 70ms
Set CursorBlink false

Hide
Type "source docs/demo/.zshrc && clear" Enter
Sleep 1.5s
Sleep 500ms
Show

Sleep 900ms
Type "kubectl edit-secret -n payments "
Sleep 500ms
Tab
Sleep 2.2s
Type@110ms "po"
Sleep 300ms
Tab
Sleep 700ms
Type " --restart"
Sleep 600ms
Enter

# vim opens with decoded values
Sleep 3s
Type "/Sup3r"
Sleep 400ms
Enter
Sleep 900ms
Type "C"
Sleep 300ms
Type@95ms "n3w-r0tated-p4ss"
Sleep 500ms
Escape
Sleep 900ms
Type ":wq"
Sleep 400ms
Enter

# summary, context-aware prompt
Sleep 2.8s
Type "y"
Sleep 350ms
Enter
Sleep 4s
