package command

import (
	"io"
	"os"
)

// pollableInput returns file as it is: Windows pipes have no non-blocking
// mode for Go's poller, so a pending read on a quiet writer cannot be
// interrupted on quit.
func pollableInput(file *os.File) io.ReadCloser { return file }
