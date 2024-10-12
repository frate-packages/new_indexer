package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Feature struct representing each feature
type Feature struct {
	Description      string   `json:"description"`
	RequiredFeatures []string `json:"required_features,omitempty"`
	Dependencies     []string `json:"dependencies,omitempty"` // Change to []string
}

// Package struct representing the package structure
type Package struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Versions     []string           `json:"versions"`
	Description  string             `json:"description"`
	GitURL       string             `json:"gitURL"`
	License      string             `json:"license"`
	Supports     string             `json:"supports,omitempty"`
	Stars        int                `json:"stars"`
	LastModified string             `json:"last_modified"`
	Dependencies []string           `json:"dependencies"` // Change to []string
	Features     map[string]Feature `json:"features,omitempty"`
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
	Description  json.RawMessage `json:"Description"` // Can be a string or an array
	GitURL       string          `json:"homepage"`
	License      string          `json:"License"`
	Supports     string          `json:"Supports,omitempty"`
	Stars        int             `json:"Stars"`
	LastModified string          `json:"LastModified"`
	Dependencies json.RawMessage `json:"Dependencies"` // Can be a list of strings or objects
	Features     json.RawMessage `json:"Features,omitempty"`
}

// Transform method converts RawPackage to the refined Package structure
func (rp *RawPackage) Transform() (Package, error) {
	// Handle dependencies
	var dependencyList []string

	// Handle mixed dependencies (strings and objects)
	var mixedDeps []interface{}
	if err := json.Unmarshal(rp.Dependencies, &mixedDeps); err != nil {
		return Package{}, err
	}

	for _, dep := range mixedDeps {
		switch depType := dep.(type) {
		case string:
			// Simple string dependency
			if depType != "vcpkg-cmake" && depType != "vcpkg-msbuild" && depType != "vcpkg-cmake-config" {
				dependencyList = append(dependencyList, depType)
			}
		case map[string]interface{}:
			// Dependency with additional fields (e.g., platform, host)
			depName := depType["name"].(string)
			if depName == "vcpkg-cmake" || depName == "vcpkg-cmake-config" || depName == "vcpkg-msbuild" {
				continue // Skip vcpkg-cmake dependencies
			}

			// Add the dependency name directly
			dependencyList = append(dependencyList, depName)
		default:
			fmt.Printf("Unknown dependency type: %T\n", depType)
		}
	}

	// Handle `Description` as string or array
	var description string
	if err := json.Unmarshal(rp.Description, &description); err != nil {
		var descriptions []string
		if err := json.Unmarshal(rp.Description, &descriptions); err == nil {
			// Join the array into a single string
			description = strings.Join(descriptions, ", ")
		} else {
			return Package{}, err
		}
	}

	// Handle features
	featuresMap := make(map[string]Feature)
	if len(rp.Features) > 0 {
		// Check if the features field is an array or a map
		var featureData interface{}
		if err := json.Unmarshal(rp.Features, &featureData); err != nil {
			return Package{}, err
		}

		switch featureType := featureData.(type) {
		case map[string]interface{}:
			// Normal feature map
			for featName, feat := range featureType {
				featMap := feat.(map[string]interface{})

				// Handle description as string or array
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

				// Handle feature dependencies
				var featureDeps []string
				var requiredFeatures []string
				if depList, ok := featMap["dependencies"].([]interface{}); ok {
					for _, dep := range depList {
						switch depVal := dep.(type) {
						case string:
							// Simple string dependency
							featureDeps = append(featureDeps, depVal)
						case map[string]interface{}:
							depName := depVal["name"].(string)
							// Check if the feature depends on itself (self-referential dependency)
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
		case []interface{}:
			// Features is an array, handle this case gracefully
			fmt.Println("Features is an array; processing as needed.")
		default:
			fmt.Printf("Unknown feature type: %T\n", featureType)
		}
	}

	return Package{
		Name:         rp.Name,
		Version:      rp.Version,
		Description:  description,
		GitURL:       rp.GitURL,
		License:      rp.License,
		Supports:     rp.Supports,
		Stars:        rp.Stars,
		LastModified: rp.LastModified,
		Dependencies: dependencyList, // Change to use slice of strings
		Features:     featuresMap,
	}, nil
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

	// Create a slice for transformed packages
	var transformedPackages []Package

	for _, rawPkg := range root.Source {
		// Transform RawPackage into Package
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
