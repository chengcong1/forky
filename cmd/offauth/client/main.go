package main

import (
	"github.com/chengcong1/forky/pkg/offauth"
)

func main() {
	err := offauth.GenerateRequestFile()
	if err != nil {
		panic(err)
	}
}
