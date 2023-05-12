package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type Cluster struct {
	center color.RGBA
	points []color.RGBA
}

type ImageDetails struct {
	ID     string
	QID    string
	Colors []ColorDetails
}

type ColorDetails struct {
	RGBAColor  string
	HexColor   string
	Percentage float64
}

// Function to calculate the Euclidean distance between two colors
func colorDistance(c1, c2 color.RGBA) float64 {
	rDiff := float64(c1.R) - float64(c2.R)
	gDiff := float64(c1.G) - float64(c2.G)
	bDiff := float64(c1.B) - float64(c2.B)
	return rDiff*rDiff + gDiff*gDiff + bDiff*bDiff
}

// Function to find the closest cluster for a color
func findClosestCluster(color color.RGBA, clusters []Cluster) int {
	minDistance := colorDistance(color, clusters[0].center)
	closestCluster := 0

	for i := 1; i < len(clusters); i++ {
		distance := colorDistance(color, clusters[i].center)
		if distance < minDistance {
			minDistance = distance
			closestCluster = i
		}
	}

	return closestCluster
}

func saveQID(filename string, img image.Image) string {
	// Create a new file in the same directory, with the same name but with "_quantized" appended
	newFilename := strings.TrimSuffix(filename, filepath.Ext(filename)) + "_quantized.jpg"
	outFile, err := os.Create(newFilename)
	if err != nil {
		log.Fatal(err)
	}
	defer outFile.Close()

	// Convert the image to JPEG and save it
	err = jpeg.Encode(outFile, img, &jpeg.Options{Quality: 100})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Quantized image saved to %s\n", newFilename)
	return newFilename
}

func updateClusterCenters(clusters []Cluster) {
	var wg sync.WaitGroup
	// Define the number of goroutines to use
	numRoutines := runtime.NumCPU()
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := i; j < len(clusters); j += numRoutines {
				points := clusters[j].points
				if len(points) > 0 {
					rSum, gSum, bSum, count := 0, 0, 0, 0
					for _, p := range points {
						rSum += int(p.R)
						gSum += int(p.G)
						bSum += int(p.B)
						count++
					}
					clusters[j].center.R = uint8(rSum / count)
					clusters[j].center.G = uint8(gSum / count)
					clusters[j].center.B = uint8(bSum / count)
				}
			}
		}(i)
	}
	wg.Wait()
}

// Function to perform color quantization using K-means clustering
func quantizeColors(img image.Image, numClusters int) image.Image {
	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y

	// Initialize the clusters
	clusters := make([]Cluster, numClusters)
	for i := 0; i < numClusters; i++ {
		x := rand.Intn(width)
		y := rand.Intn(height)
		clusters[i].center = color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	}

	// Run K-means for a fixed number of iterations
	for iter := 0; iter < 10; iter++ {
		// Clear points in each cluster
		for i := range clusters {
			clusters[i].points = []color.RGBA{}
		}

		// Assign pixels to the closest cluster
		for x := 0; x < width; x++ {
			for y := 0; y < height; y++ {
				color := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
				closestCluster := findClosestCluster(color, clusters)
				clusters[closestCluster].points = append(clusters[closestCluster].points, color)
			}
		}

		// Update the cluster centers
		updateClusterCenters(clusters)
	}

	// Create the quantized image using the final cluster centers
	quantized := image.NewRGBA(bounds)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			color := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			closestCluster := findClosestCluster(color, clusters)
			quantized.Set(x, y, clusters[closestCluster].center)
		}
	}

	return quantized
}

func findDominantColors(img image.Image, numColors int) ImageDetails {
	var thisImageDetails ImageDetails
	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y
	totalPixels := width * height

	// Calculate the frequency of each color in the image
	colorFreq := make(map[color.RGBA]int)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			color := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			colorFreq[color]++
		}
	}

	// Sort the colors based on frequency
	sortedColors := make([]color.RGBA, 0, len(colorFreq))
	for c := range colorFreq {
		sortedColors = append(sortedColors, c)
	}
	sort.Slice(sortedColors, func(i, j int) bool {
		return colorFreq[sortedColors[i]] > colorFreq[sortedColors[j]]
	})

	// Get the top numColors dominant colors
	if numColors <= 0 || numColors > len(sortedColors) {
		numColors = len(sortedColors)
	}

	// Check if there are no dominant colors
	if numColors == 0 {
		return thisImageDetails
	}

	dominantColors := sortedColors[:numColors]

	// Calculate color percentages
	colorPercentages := make([]float64, numColors)
	for i, c := range dominantColors {
		colorPercentages[i] = float64(colorFreq[c]) / float64(totalPixels) * 100
		colorPercentages[i] = math.Round(colorPercentages[i]*100) / 100
	}

	colors := []ColorDetails{}

	for i, c := range dominantColors {
		color := ColorDetails{
			RGBAColor:  fmt.Sprintf("%v", c),
			HexColor:   fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B),
			Percentage: colorPercentages[i],
		}
		colors = append(colors, color)
	}

	thisImageDetails = ImageDetails{
		Colors: colors,
	}

	return thisImageDetails
}

func processImage(filename string, numColors int, outputFormat string) ImageDetails {
	// Open the image file
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Decode the image based on the file format
	var img image.Image
	if strings.ToLower(filepath.Ext(filename)) == ".png" {
		img, err = png.Decode(file)
		if err != nil {
			log.Fatal(err)
		}
	} else if strings.ToLower(filepath.Ext(filename)) == ".jpg" || strings.ToLower(filepath.Ext(filename)) == ".jpeg" {
		img, err = jpeg.Decode(file)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatal("Unsupported image file format:", filepath.Ext(filename))
	}

	// Perform color quantization using K-means clustering with 16 clusters
	quantized := quantizeColors(img, 16)

	// Save the quantized image
	quantizedFilename := saveQID(filename, quantized)

	thisImageDetails := findDominantColors(quantized, numColors)
	thisImageDetails.ID = filename
	thisImageDetails.QID = quantizedFilename // set the quantized image filename

	return thisImageDetails
}

func writeJsonToFile(fileName string, imageColors []ImageDetails) {
	jsonData, err := json.MarshalIndent(imageColors, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	err = ioutil.WriteFile(fileName, jsonData, 0644)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("JSON output written to %s\n", fileName)
}

func writeHtmlToFile(fileName string, imageColors []ImageDetails) {
	tmpl, err := template.ParseFiles("template.html")
	if err != nil {
		log.Println(err)
		return
	}

	f, err := os.Create(fileName)
	if err != nil {
		log.Println(err)
		return
	}
	defer f.Close()

	err = tmpl.Execute(f, imageColors)
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Printf("Output written to %s\n", fileName)
}

func main() {
	var numColors int
	var outputFormat string
	flag.IntVar(&numColors, "n", 16, "Number of prominent colors to find")
	flag.StringVar(&outputFormat, "f", "hex", "Color value format: 'hex' or 'rgb'")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: go run main.go [flags] <input_file_or_directory>")
		return
	}

	inputPath := flag.Args()[0]

	// Check if the input is a file or a directory
	fileInfo, err := os.Stat(inputPath)
	if err != nil {
		log.Fatal(err)
	}

	var allImageColors []ImageDetails

	if fileInfo.IsDir() {
		// Process all images in the directory
		err = filepath.Walk(inputPath, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() {
				fmt.Printf("Analysing file: %v...\n", path)
				thisImageDetails := processImage(path, numColors, outputFormat)
				thisImageDetails.ID = path
				allImageColors = append(allImageColors, thisImageDetails)
			}
			return nil
		})
		if err != nil {
			log.Fatal(err)
		}
	} else {
		// Process a single image
		fmt.Printf("Analysing file: %v...\n", inputPath)
		thisImageDetails := processImage(inputPath, numColors, outputFormat)
		thisImageDetails.ID = inputPath
		allImageColors = append(allImageColors, thisImageDetails)
	}

	for x := range allImageColors {
		fmt.Println(allImageColors[x].ID)
		for _, colorDetails := range allImageColors[x].Colors {
			fmt.Printf("Color: %v, %.2f%%\n", colorDetails.HexColor, colorDetails.Percentage)
		}
	}

	// Write the results to a JSON file
	writeJsonToFile("output.json", allImageColors)

	// Write the results to an HTML file
	writeHtmlToFile("output.html", allImageColors)

	fmt.Println("Color analysis completed successfully.")
}
