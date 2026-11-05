// Package profiles 校验公开展示资料，不承担身份识别或内容审核。
package profiles

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var ErrInvalidDisplayName = errors.New("invalid display name")

// NormalizeDisplayName 返回应保存及展示的 NFKC 昵称；失败时不返回部分结果。
func NormalizeDisplayName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidDisplayName)
	}
	name = norm.NFKC.String(name)
	// 在裁剪前拒绝禁止字符，避免把首尾换行等控制内容静默当作普通空白。
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Bidi_Control) {
			return "", fmt.Errorf("%w: control, line/paragraph separator or bidi control character", ErrInvalidDisplayName)
		}
	}
	name = strings.TrimSpace(name)
	if count := utf8.RuneCountInString(name); count < 1 || count > 32 || len(name) > 128 {
		return "", fmt.Errorf("%w: must contain 1–32 code points and at most 128 bytes", ErrInvalidDisplayName)
	}
	for _, r := range name {
		// 格式字符、附加标记和默认可忽略字符可以保留，但不能独自构成可见昵称。
		// U+2800 是不属于空白或默认可忽略属性的盲文空格。
		if !unicode.IsSpace(r) && !unicode.IsMark(r) && !unicode.In(r, unicode.Cf, unicode.Other_Default_Ignorable_Code_Point) && r != '\u2800' {
			return name, nil
		}
	}
	return "", fmt.Errorf("%w: must contain visible content", ErrInvalidDisplayName)
}
