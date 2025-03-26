package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jung-kurt/gofpdf"
)

func collectGoFiles(root string) (map[string]string, error) {
	files := make(map[string]string)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(content)
		}
		return nil
	})

	return files, err
}

func generatePDF(files map[string]string, output string) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetFont("Courier", "", 10)

	for path, content := range files {
		pdf.AddPage()
		pdf.Cell(40, 10, path)
		pdf.Ln(10)

		lines := strings.Split(content, "\n")
		for _, line := range lines {
			pdf.MultiCell(0, 4.5, line, "", "L", false)
		}
	}

	return pdf.OutputFileAndClose(output)
}

func main() {
	root := "." // можно заменить на путь до проекта
	output := "project_code.pdf"

	files, err := collectGoFiles(root)
	if err != nil {
		fmt.Println("Ошибка при сборе файлов:", err)
		return
	}

	err = generatePDF(files, output)
	if err != nil {
		fmt.Println("Ошибка при создании PDF:", err)
		return
	}

	fmt.Println("✅ PDF создан:", output)
}
