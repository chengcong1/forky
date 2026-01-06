package img

import "github.com/J-Siu/go-png2ico/v2/p2i"

func PNGToICO(pngFile, icoFile string) error {
	// icoFile = ICO file to be created/overwritten
	// ICO object must be initialized with New()
	ico := new(p2i.ICO).New(icoFile)
	// pngFile = PNG file path
	// this steps can be repeated multiple times
	ico.AddPngFile(pngFile)
	// Create ICO file with all PNG loaded
	ico.Write()
	return ico.Err
	// return nil
}
