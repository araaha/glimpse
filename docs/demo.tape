# Recorded by make demo, which shrinks the GIF for the README.
Output demo-raw.gif

Set Shell bash
Set FontSize 16
Set Width 960
Set Height 540
Set Padding 20
Set TypingSpeed 60ms
Set Theme "Catppuccin Mocha"

Hide
# A plain shell, without the recording user's prompt or readline settings.
Type `INPUTRC=/dev/null PATH="$PWD:$PATH" exec bash --norc --noprofile`
Enter
Type `PS1='$ '`
Enter
Type "clear"
Enter
Show

# One endless line of random characters: glimpse wraps it as it arrives.
Type "LC_ALL=C tr -dc 'a-zA-Z0-9' < /dev/urandom | glimpse -N"
Sleep 500ms
Enter
Sleep 2s

# Regular expression search, then the next matches.
Type "/[0-9]{5}"
Sleep 500ms
Enter
Sleep 1.5s
Type "n"
Sleep 1s
Type "n"
Sleep 1.5s

# Follow the end of the stream as it grows.
Type "F"
Sleep 2s

# The bindings in effect.
Type "g?"
Sleep 3s
Escape
Sleep 500ms
Type "q"
Sleep 1s
