package rememberer

const RemembererWordURIVariableId = "wordId"

const (

	// ErrKeyRemembererError error key placeholder [R-001]
	ErrKeyRemembererError string = "RemembererError"

	// ErrKeyWordWithIdNotFound returned when unable to find word with matching Id [R-002]
	ErrKeyWordWithIdNotFound string = "WordWithIdNotFound"

	// ErrKeyWordWithNameNotFound returned when unable to find word with matching name [R-003]
	ErrKeyWordWithNameNotFound string = "WordWithNameNotFound"

	// ErrKeyWordAlreadyExists returned when the word can be found in repository [R-004]
	ErrKeyWordAlreadyExists string = "WordAlreadyExists"

	// ErrKeyInvalidWordId returned when an error occurs when carrying out an action around validating or getting
	// a word Id [R-005]
	ErrKeyInvalidWordId string = "InvalidWordId"

	// ErrKeyInvalidWordBody returned when error occurs while decoding word request body [R-006]
	ErrKeyInvalidWordBody string = "InvalidCreateWordBody"

	// ErrKeyInvalidQueryParam returned when error occurs while validating query params on request [R-007]
	ErrKeyInvalidQueryParam string = "InvalidQueryParam"
)
