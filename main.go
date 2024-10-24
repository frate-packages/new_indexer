package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	_ "github.com/mattn/go-sqlite3"
)

type Feature struct {
	Description      string   `json:"description"`
	RequiredFeatures []string `json:"required_features,omitempty"`
	Dependencies     []string `json:"dependencies,omitempty"`
}

type Package struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Description  string             `json:"description"`
	GitURL       string             `json:"git_url"`
	License      string             `json:"license"`
	Supports     string             `json:"supports,omitempty"`
	Stars        int                `json:"stars"`
	LastModified string             `json:"last_modified"`
	CMakeTarget  string             `json:"cmake_target,omitempty"`
	Dependencies []string           `json:"dependencies"`
	Features     map[string]Feature `json:"features,omitempty"`
}

var db *sql.DB

func init() {
	var err error
	db, err = sql.Open("sqlite3", "data.sql")
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}
}

func listPackages(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT name, version, description, git_url, license, supports, stars, last_modified, cmake_target FROM packages")
	if err != nil {
		http.Error(w, "Error querying database", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var packages []Package
	for rows.Next() {
		var pkg Package
		err := rows.Scan(&pkg.Name, &pkg.Version, &pkg.Description, &pkg.GitURL, &pkg.License, &pkg.Supports, &pkg.Stars, &pkg.LastModified, &pkg.CMakeTarget)
		if err != nil {
			http.Error(w, "Error scanning package row", http.StatusInternalServerError)
			return
		}

		pkg.Dependencies = getPackageDependencies(pkg.Name)
		pkg.Features = getPackageFeatures(pkg.Name)
		packages = append(packages, pkg)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(packages)
}

func getPackageDependencies(packageName string) []string {
	var dependencies []string
	rows, err := db.Query("SELECT dependency_name FROM dependencies WHERE package_name = ?", packageName)
	if err != nil {
		return dependencies
	}
	defer rows.Close()

	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err == nil {
			dependencies = append(dependencies, dependency)
		}
	}
	return dependencies
}

func getPackageFeatures(packageName string) map[string]Feature {
	features := make(map[string]Feature)
	rows, err := db.Query("SELECT feature_name, description FROM features WHERE package_name = ?", packageName)
	if err != nil {
		return features
	}
	defer rows.Close()

	for rows.Next() {
		var featureName, description string
		if err := rows.Scan(&featureName, &description); err == nil {
			features[featureName] = Feature{
				Description:      description,
				Dependencies:     getFeatureDependencies(packageName, featureName),
				RequiredFeatures: []string{},
			}
		}
	}
	return features
}

func getFeatureDependencies(packageName, featureName string) []string {
	var dependencies []string
	rows, err := db.Query("SELECT dependency_name FROM feature_dependencies WHERE package_name = ? AND feature_name = ?", packageName, featureName)
	if err != nil {
		return dependencies
	}
	defer rows.Close()

	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err == nil {
			dependencies = append(dependencies, dependency)
		}
	}
	return dependencies
}

func createPackage(w http.ResponseWriter, r *http.Request) {
	var pkg Package
	if err := json.NewDecoder(r.Body).Decode(&pkg); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	_, err := db.Exec(`INSERT INTO packages (name, version, description, git_url, license, supports, stars, last_modified, cmake_target)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pkg.Name, pkg.Version, pkg.Description, pkg.GitURL, pkg.License, pkg.Supports, pkg.Stars, pkg.LastModified, pkg.CMakeTarget)
	if err != nil {
		http.Error(w, "Error inserting package", http.StatusInternalServerError)
		return
	}

	insertDependencies(pkg.Name, pkg.Dependencies)
	insertFeatures(pkg.Name, pkg.Features)

	w.WriteHeader(http.StatusCreated)
}

func insertDependencies(packageName string, dependencies []string) {
	for _, dep := range dependencies {
		_, err := db.Exec("INSERT INTO dependencies (package_name, dependency_name) VALUES (?, ?)", packageName, dep)
		if err != nil {
			log.Printf("Error inserting dependency %s for package %s: %v", dep, packageName, err)
		}
	}
}

func insertFeatures(packageName string, features map[string]Feature) {
	for featName, feat := range features {
		_, err := db.Exec("INSERT INTO features (package_name, feature_name, description) VALUES (?, ?, ?)", packageName, featName, feat.Description)
		if err != nil {
			log.Printf("Error inserting feature %s for package %s: %v", featName, packageName, err)
			continue
		}

		for _, dep := range feat.Dependencies {
			_, err := db.Exec("INSERT INTO feature_dependencies (package_name, feature_name, dependency_name) VALUES (?, ?, ?)", packageName, featName, dep)
			if err != nil {
				log.Printf("Error inserting feature dependency %s for feature %s in package %s: %v", dep, featName, packageName, err)
			}
		}
	}
}

func deletePackage(w http.ResponseWriter, r *http.Request) {
	packageName := r.URL.Query().Get("name")
	if packageName == "" {
		http.Error(w, "Missing package name", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("DELETE FROM packages WHERE name = ?", packageName)
	if err != nil {
		http.Error(w, "Error deleting package", http.StatusInternalServerError)
		return
	}
	_, _ = db.Exec("DELETE FROM dependencies WHERE package_name = ?", packageName)
	_, _ = db.Exec("DELETE FROM features WHERE package_name = ?", packageName)
	_, _ = db.Exec("DELETE FROM feature_dependencies WHERE package_name = ?", packageName)

	w.WriteHeader(http.StatusOK)
}

func getPackage(w http.ResponseWriter, r *http.Request) {
	packageName := r.URL.Query().Get("name")
	if packageName == "" {
		http.Error(w, "Missing package name", http.StatusBadRequest)
		return
	}

	var pkg Package
	err := db.QueryRow(`SELECT name, version, description, git_url, license, supports, stars, last_modified, cmake_target 
						FROM packages WHERE name = ?`, packageName).Scan(
		&pkg.Name, &pkg.Version, &pkg.Description, &pkg.GitURL, &pkg.License, &pkg.Supports, &pkg.Stars, &pkg.LastModified, &pkg.CMakeTarget,
	)
	if err == sql.ErrNoRows {
		http.Error(w, "Package not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Error querying database", http.StatusInternalServerError)
		return
	}

	pkg.Dependencies = getPackageDependencies(packageName)
	pkg.Features = getPackageFeatures(packageName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pkg)
}

func main() {
	http.HandleFunc("/packages", listPackages)
	http.HandleFunc("/package", getPackage) // New route to get a specific package
	http.HandleFunc("/package/create", createPackage)
	http.HandleFunc("/package/delete", deletePackage)

	fmt.Println("Starting server on :8000...")
	if err := http.ListenAndServe(":8000", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
