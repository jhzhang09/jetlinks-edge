package handler

// errMissingField 必填字段缺失。
func errMissingField(fields string) error {
	return &fieldErr{msg: "missing required field: " + fields}
}

type fieldErr struct{ msg string }

func (e *fieldErr) Error() string { return e.msg }
