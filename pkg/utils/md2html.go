package utils

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
	"github.com/shurcooL/github_flavored_markdown"
	"github.com/yuin/goldmark"
)

/*
1、MarkdownToHtml_blackfriday_safe(markdown []byte) string MD转html 使用 blackfriday + bluemonday
2、MarkdownToHtml_blackfriday(markdown []byte) string MD转html 使用 blackfriday
3、MarkdownToHtml_goldmark(markdown []byte) (string, error) MD转html 使用 goldmark
4、MarkdownToHtml_gfm(markdown []byte) string MD转html 使用 github_flavored_markdown
*/

// MD转html 使用 blackfriday + bluemonday
func MarkdownToHtml_blackfriday_safe(markdown []byte) string {
	unsafe := blackfriday.Run(markdown)
	html := bluemonday.UGCPolicy().SanitizeBytes(unsafe)
	// return string(blackfriday.Run(markdown, blackfriday.WithNoExtensions()))
	return string(html)

}

// MD转html 使用 blackfriday
func MarkdownToHtml_blackfriday(markdown []byte) string {
	return string(blackfriday.Run(markdown))
}

// MD转html 使用 goldmark
func MarkdownToHtml_goldmark(markdown []byte) (string, error) {
	var buf bytes.Buffer
	err := goldmark.Convert(markdown, &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), err
}

// MD转html 使用 github_flavored_markdown
func MarkdownToHtml_gfm(markdown []byte) string {
	return string(github_flavored_markdown.Markdown(markdown))
}
