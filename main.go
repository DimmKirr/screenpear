/*
 * Copyright 2025 Dmitry Kireev <dmitry@atd.sh>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/macos/vision"
	"log"
	"strings"
)

func recognizeText(imagePath string) string {
	// Load image as NSImage
	nsImage := appkit.NSImage_Alloc().InitWithContentsOfFile(core.String(imagePath))
	if nsImage.ID == 0 {
		log.Fatal("Failed to load image.")
		return ""
	}

	// Convert NSImage to CGImage
	imageData := nsImage.TIFFRepresentation()
	bitmap := appkit.NSBitmapImageRep_Alloc().InitWithData(imageData)
	if bitmap.ID == 0 {
		log.Fatal("Failed to create bitmap representation.")
		return ""
	}

	cgImage := bitmap.CGImage()
	if cgImage.ID == 0 {
		log.Fatal("Failed to convert NSImage to CGImage.")
		return ""
	}

	// Create VNImageRequestHandler with CGImage
	handler := vision.VNImageRequestHandler_Alloc().InitWithCGImageOptions(cgImage, foundation.NSDictionary{})
	if handler.ID == 0 {
		log.Fatal("Failed to create VNImageRequestHandler.")
		return ""
	}

	// Create text recognition request
	request := vision.VNRecognizeTextRequest_Alloc().Init()
	if request.ID == 0 {
		log.Fatal("Failed to create VNRecognizeTextRequest.")
		return ""
	}

	// Set recognition level to accurate
	request.SetRecognitionLevel(vision.VNRequestTextRecognitionLevelAccurate)

	// Perform OCR request
	success, err := handler.PerformRequestsError([]foundation.NSObject{request})
	if !success {
		log.Fatalf("OCR request failed: %v", err)
		return ""
	}

	// Extract recognized text
	var extractedText []string
	results := request.Results()
	if results.ID == 0 {
		log.Fatal("No OCR results found.")
		return ""
	}

	for _, obsObj := range results.Objects() {
		observation := vision.VNRecognizedTextObservation{NSObject: obsObj}
		topCandidates := observation.TopCandidates(1).Objects()

		if len(topCandidates) > 0 {
			text := vision.VNRecognizedText{NSObject: topCandidates[0]}.String()
			extractedText = append(extractedText, text)
		}
	}

	// Return extracted text
	return strings.Join(extractedText, "\n")
}

func main() {
	imagePath := "/Users/dmitry/dev/automationd/lab.atd.sh/screenpear/data/input/digitalocean-01-input.jpg"
	text := recognizeText(imagePath)
	if text == "" {
		fmt.Println("No text detected.")
	} else {
		fmt.Println("Recognized Text:\n", text)
	}
}
