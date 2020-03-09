package facebook

// ArchiveError implemnts interface CodeError
type ArchiveError struct {
	CodeString string `json:"code"`
	Message    string `json:"message"`
}

func (a *ArchiveError) Code() string {
	return a.CodeString
}
func (a *ArchiveError) Error() string {
	return a.Message
}

func NewArchiveError(code, message string) *ArchiveError {
	return &ArchiveError{
		CodeString: code,
		Message:    message,
	}
}

var (
	ErrFailToCreateArchive   = NewArchiveError("FAIL_TO_CREATE_ARCHIVE", "fail to create archive")
	ErrFailToParseArchive    = NewArchiveError("FAIL_TO_PARSE_ARCHIVE", "fail to parse archive")
	ErrFailToDownloadArchive = NewArchiveError("FAIL_TO_DOWNLOAD_ARCHIVE", "fail to download archive")
	ErrFailToExtractPost     = NewArchiveError("FAIL_TO_EXTRACT_POST", "fail to extract post")
	ErrFailToExtractReaction = NewArchiveError("FAIL_TO_EXTRACT_REACTION", "fail to extract reaction")
)
