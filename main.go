package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
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
	License      string             `json:"license,omitempty"`
	Supports     string             `json:"supports,omitempty"`
	Stars        int                `json:"stars,omitempty"`
	LastModified string             `json:"last_modified,omitempty"`
	CMakeTarget  string             `json:"cmake_target,omitempty"`
	Dependencies []string           `json:"dependencies"`
	Features     map[string]Feature `json:"features,omitempty"`
}

var db *sql.DB
var ctx = context.Background()



var redisClient *redis.Client

func initRedis() *redis.Client { 
	// Read Redis host and port from environment variables
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost" // Fallback to localhost if not set
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379" // Fallback to default Redis port if not set
	}

	// Construct Redis address
	redisAddr := redisHost + ":" + redisPort

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	return redisClient 
}



func init() {
	var err error
	var databaseURL string = "./data.sql"
	var databaseDriver string = "sqlite3"
	if os.Getenv("DATABASE_URL") != "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if os.Getenv("DATABASE_DRIVER") != "" {
		databaseDriver = os.Getenv("DATABASE_DRIVER")
	}
	db, err = sql.Open(databaseDriver, databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}

	redisClient = initRedis() 
	_, err = redisClient.Ping(ctx).Result()
	if err != nil {
		fmt.Println("Failed to connect to Redis: %v", err)
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
}

func listPackages(w http.ResponseWriter, r *http.Request) {
	// Check Redis first
	cachedList, err := redisClient.Get(ctx, "all_packages").Result()
	if err == nil {
		// Cache hit: Deserialize and return the cached list
		var packages []Package
		if json.Unmarshal([]byte(cachedList), &packages) == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(packages)
			return
		}
	}

	// Cache miss: Fetch from SQLite
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

		// Add dependencies and features to each package
		pkg.Dependencies = getPackageDependencies(pkg.Name)
		pkg.Features = getPackageFeatures(pkg.Name)
		packages = append(packages, pkg)
	}

	// Cache the list of packages in Redis
	serializedPackages, _ := json.Marshal(packages)
	redisClient.Set(ctx, "all_packages", serializedPackages, 10*time.Minute) // Cache expires in 10 minutes

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

	pkg.LastModified = time.Now().UTC().String()
	_, err := db.Exec(`INSERT INTO packages (name, version, description, git_url, license, supports, stars, last_modified, cmake_target)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pkg.Name, pkg.Version, pkg.Description, pkg.GitURL, pkg.License, pkg.Supports, pkg.Stars, pkg.LastModified, pkg.CMakeTarget)
	if err != nil {
		http.Error(w, "Error inserting package", http.StatusInternalServerError)
		return
	}

	insertDependencies(pkg.Name, pkg.Dependencies)
	insertFeatures(pkg.Name, pkg.Features)

	// Invalidate Redis cache for the entire package list
	redisClient.Del(ctx, "all_packages")

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

	// Invalidate Redis cache for the entire package list
	redisClient.Del(ctx, "all_packages")

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

	pkg.Dependencies = getPackageDependencies(pkg.Name)
	pkg.Features = getPackageFeatures(pkg.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pkg)
}

func main() {
	initRedis()
	http.HandleFunc("/packages", listPackages)
	http.HandleFunc("/packages/create", createPackage)
	http.HandleFunc("/packages/delete", deletePackage)
	http.HandleFunc("/package", getPackage)

	fmt.Println("Server is running on port 8000...")
	log.Fatal(http.ListenAndServe(":8000", nil))
}
