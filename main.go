package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Feature struct representing each feature
type Feature struct {
	Description      string   `json:"description"`
	RequiredFeatures []string `json:"required_features,omitempty"`
	Dependencies     []string `json:"dependencies,omitempty"`
}

// Package struct representing the package structure
type Package struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Tag          string             `json:"tag"`
	Versions     []string           `json:"versions"`
	Description  string             `json:"description"`
	GitURL       string             `json:"gitURL"`
	License      string             `json:"license"`
	Supports     string             `json:"supports,omitempty"`
	Stars        int                `json:"stars"`
	LastModified string             `json:"last_modified"`
	Dependencies []string           `json:"dependencies"`
	Features     map[string]Feature `json:"features,omitempty"`
	CMakeTarget  string             `json:"cmake_target,omitempty"`
}

// Root struct representing the entire JSON structure
type Root struct {
	Baseline string       `json:"Baseline"`
	Size     int          `json:"Size"`
	Source   []RawPackage `json:"Source"`
}

// RawPackage struct to handle mixed types in `Dependencies`, `Description`, and `Features`
type RawPackage struct {
	Name         string          `json:"Name"`
	Version      string          `json:"Version"`
	Description  json.RawMessage `json:"Description"`
	GitURL       string          `json:"homepage"`
	License      string          `json:"License"`
	Supports     string          `json:"Supports,omitempty"`
	Stars        int             `json:"Stars"`
	LastModified string          `json:"LastModified"`
	Dependencies json.RawMessage `json:"Dependencies"`
	Features     json.RawMessage `json:"Features,omitempty"`
}

var versionRegexes = []*regexp.Regexp{
	regexp.MustCompile(`^v?\d+\.\d+\.\d+$`),             // v1.2.3 or 1.2.3
	regexp.MustCompile(`^v?\d+\.\d+$`),                  // v1.2 or 1.2
	regexp.MustCompile(`[A-Za-z]+-?\d+_\d+_\d+$`),       // word-1_2_3 or word1_2_3
	regexp.MustCompile(`[A-Za-z]+-?\d+\.\d+\.\d+$`),     // word-1.2.3 or word1.2.3
	regexp.MustCompile(`[A-Za-z]+_?\d+\.\d+\.\d+$`),     // word_1.2.3 or word1.2.3
	regexp.MustCompile(`[A-Za-z]+_?\d+\.\d+$`),          // word_1.2 or word1.2
	regexp.MustCompile(`^(master|latest|stable|main)$`), // Specific keywords
}

// Parse remote git tags
func parseRemoteLsTags(output string) []string {
	lines := strings.Split(output, "\n")
	var tags []string

	for _, line := range lines {
		if strings.Contains(line, "refs/tags/") || strings.Contains(line, "refs/heads/") {
			parts := strings.Split(line, "/")
			tag := parts[len(parts)-1]

			if validateVersionName(tag) {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// Validate if a version matches known version patterns
func validateVersionName(version string) bool {
	for _, regex := range versionRegexes {
		if regex.MatchString(version) {
			return true
		}
	}
	return false
}


// Function to extract remote versions from git, returns empty version if GitURL is invalid or inaccessible
func getRemoteVersions(packageInfo *Package) error {
	if packageInfo.GitURL != "" {
		cmd := exec.Command("git", "ls-remote", packageInfo.GitURL)

		// Log the URL for debugging purposes
		fmt.Printf("Fetching tags for repository: %s\n", packageInfo.GitURL)

		out, err := cmd.CombinedOutput()
		if err != nil {
			// Log the error, but return an empty array instead of failing
			fmt.Printf("Error running git ls-remote for %s: %s. Returning empty versions.\n", packageInfo.GitURL, string(out))
			packageInfo.Versions = []string{} // Return empty versions
			return nil // No error returned, continue processing
		}

		tags := parseRemoteLsTags(string(out))
		if len(tags) > 0 {
			// Set the latest tag as the version
			packageInfo.Version = tags[len(tags)-1]
			packageInfo.Versions = tags
		} else {
			// If no tags are found, return empty versions
			fmt.Printf("No valid git tags found for %s. Returning empty versions.\n", packageInfo.GitURL)
			packageInfo.Versions = []string{}
		}
		return nil
	}
	// No Git URL provided, return empty versions
	fmt.Printf("No git URL provided for %s. Returning empty versions.\n", packageInfo.Name)
	packageInfo.Versions = []string{}
	return nil
}


// Function to map known dependency names to CMake target names
func MapDependencyToCMakeTarget(depName string) string {
	cmakeTargetMap := map[string]string{
		"boost-asio": "Boost::asio",
		"boost-system": "Boost::system",
		"openssl": "OpenSSL::SSL",
		// Add more mappings as needed...
	}

	if target, exists := cmakeTargetMap[depName]; exists {
		return target
	}
	return depName // If no mapping exists, return the original name
}

// Transform method converts RawPackage to the refined Package structure
func (rp *RawPackage) Transform() (Package, error) {
	var dependencyList []string

	// Handle mixed dependencies (strings and objects)
	var mixedDeps []interface{}
	if err := json.Unmarshal(rp.Dependencies, &mixedDeps); err != nil {
		return Package{}, err
	}

	for _, dep := range mixedDeps {
		switch depType := dep.(type) {
		case string:
			dependencyList = append(dependencyList, depType)
		case map[string]interface{}:
			depName := depType["name"].(string)
			dependencyList = append(dependencyList, depName)
		default:
			fmt.Printf("Unknown dependency type: %T\n", depType)
		}
	}

	// Handle description
	var description string
	if err := json.Unmarshal(rp.Description, &description); err != nil {
		var descriptions []string
		if err := json.Unmarshal(rp.Description, &descriptions); err == nil {
			description = strings.Join(descriptions, ", ")
		} else {
			return Package{}, err
		}
	}

	// Get CMake target
	cmakeTarget := MapDependencyToCMakeTarget(rp.Name)

	// Handle features
	featuresMap := make(map[string]Feature)
	if len(rp.Features) > 0 {
		var featureData interface{}
		if err := json.Unmarshal(rp.Features, &featureData); err != nil {
			return Package{}, err
		}

		switch featureType := featureData.(type) {
		case map[string]interface{}:
			for featName, feat := range featureType {
				featMap := feat.(map[string]interface{})

				var featureDescription string
				switch desc := featMap["description"].(type) {
				case string:
					featureDescription = desc
				case []interface{}:
					var descArray []string
					for _, d := range desc {
						descArray = append(descArray, d.(string))
					}
					featureDescription = strings.Join(descArray, ", ")
				}

				var featureDeps []string
				var requiredFeatures []string
				if depList, ok := featMap["dependencies"].([]interface{}); ok {
					for _, dep := range depList {
						switch depVal := dep.(type) {
						case string:
							featureDeps = append(featureDeps, depVal)
						case map[string]interface{}:
							depName := depVal["name"].(string)
							if depName == rp.Name {
								requiredFeatures = append(requiredFeatures, featName)
							} else {
								featureDeps = append(featureDeps, depName)
							}
						default:
							fmt.Printf("Unknown feature dependency type: %T\n", dep)
						}
					}
				}

				featuresMap[featName] = Feature{
					Description:      featureDescription,
					RequiredFeatures: requiredFeatures,
					Dependencies:     featureDeps,
				}
			}
		}
	}

	pkg := Package{
		Name:         rp.Name,
		Version:      rp.Version, // This will be replaced by the tag we fetch
		Description:  description,
		GitURL:       rp.GitURL,
		License:      rp.License,
		Supports:     rp.Supports,
		Stars:        rp.Stars,
		LastModified: rp.LastModified,
		Dependencies: dependencyList,
		Features:     featuresMap,
		CMakeTarget:  cmakeTarget,
	}

	// Fetch the latest git tag and update the Version
	err := getRemoteVersions(&pkg)
	if err != nil {
		fmt.Printf("Error fetching remote versions for %s: %v\n", pkg.Name, err)
	}

	return pkg, nil
}

func main() {
	// Open the JSON file
	file, err := os.Open("data.json")
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	// Decode the root JSON structure
	var root Root
	if err := json.NewDecoder(file).Decode(&root); err != nil {
		fmt.Printf("Error decoding JSON: %v\n", err)
		return
	}

	var transformedPackages []Package

	for _, rawPkg := range root.Source {
		transformedPkg, err := rawPkg.Transform()
		if err != nil {
			fmt.Printf("Error transforming package: %v\n", err)
			continue
		}
		transformedPackages = append(transformedPackages, transformedPkg)
	}

	// Write the transformed packages to a new JSON file
	outputFile, err := os.Create("transformed_data.json")
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer outputFile.Close()

	if err := json.NewEncoder(outputFile).Encode(transformedPackages); err != nil {
		fmt.Printf("Error encoding transformed data: %v\n", err)
		return
	}

	fmt.Println("Transformed data written successfully!")
}

