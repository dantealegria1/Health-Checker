package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Service represents a web service to monitor
type Service struct {
    Name     string `json:"name"`
    URL      string `json:"url"`
    Interval int    `json:"interval"` // Check interval in seconds
}

// Config holds the application configuration
type Config struct {
    Services []Service `json:"services"`
}

// ServiceStatus represents the current status of a service
type ServiceStatus struct {
    Name          string
    IsHealthy     bool
    LastChecked   time.Time
    StatusCode    int
    Error         string
    LastUptime    time.Time
    UpTimePercent float64
    ResponseTime  time.Duration
    History      []HealthRecord
}

// HealthRecord represents a single health check record
type HealthRecord struct {
    Timestamp    time.Time
    IsHealthy    bool
    ResponseTime time.Duration
    StatusCode   int
}

// StatusManager manages the state of all services
type StatusManager struct {
    statuses map[string]*ServiceStatus
    mu       sync.RWMutex
}

// NewStatusManager creates a new StatusManager
func NewStatusManager() *StatusManager {
    return &StatusManager{
        statuses: make(map[string]*ServiceStatus),
    }
}

// UpdateStatus updates the status of a service
func (sm *StatusManager) UpdateStatus(status ServiceStatus) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    if existing, ok := sm.statuses[status.Name]; ok {
        // Update uptime percentage
        totalChecks := len(existing.History) + 1
        healthyChecks := 0
        for _, record := range existing.History {
            if record.IsHealthy {
                healthyChecks++
            }
        }
        if status.IsHealthy {
            healthyChecks++
        }
        status.UpTimePercent = float64(healthyChecks) / float64(totalChecks) * 100

        // Keep last 100 records
        if len(existing.History) >= 100 {
            existing.History = existing.History[1:]
        }

        record := HealthRecord{
            Timestamp:    status.LastChecked,
            IsHealthy:    status.IsHealthy,
            ResponseTime: status.ResponseTime,
            StatusCode:   status.StatusCode,
        }
        status.History = append(existing.History, record)
    } else {
        status.History = []HealthRecord{{
            Timestamp:    status.LastChecked,
            IsHealthy:    status.IsHealthy,
            ResponseTime: status.ResponseTime,
            StatusCode:   status.StatusCode,
        }}
        status.UpTimePercent = 100
    }

    if status.IsHealthy {
        status.LastUptime = status.LastChecked
    }

    sm.statuses[status.Name] = &status
}

// GetAllStatuses returns all service statuses
func (sm *StatusManager) GetAllStatuses() []ServiceStatus {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    statuses := make([]ServiceStatus, 0, len(sm.statuses))
    for _, status := range sm.statuses {
        statuses = append(statuses, *status)
    }
    return statuses
}

// loadConfig loads services from config.json
func loadConfig() (*Config, error) {
    file, err := os.ReadFile("config.json")
    if err != nil {
        return nil, err
    }

    var config Config
    err = json.Unmarshal(file, &config)
    if err != nil {
        return nil, err
    }

    return &config, nil
}

// checkService performs a health check on a single service
func checkService(service Service) ServiceStatus {
    start := time.Now()
    client := &http.Client{
        Timeout: 10 * time.Second,
    }

    resp, err := client.Get(service.URL)
    responseTime := time.Since(start)

    status := ServiceStatus{
        Name:         service.Name,
        LastChecked:  time.Now(),
        ResponseTime: responseTime,
    }

    if err != nil {
        status.IsHealthy = false
        status.Error = err.Error()
        return status
    }
    defer resp.Body.Close()

    status.StatusCode = resp.StatusCode
    status.IsHealthy = resp.StatusCode >= 200 && resp.StatusCode < 300
    return status
}

// monitorService continuously monitors a service
func monitorService(service Service, statusChan chan<- ServiceStatus) {
    ticker := time.NewTicker(time.Duration(service.Interval) * time.Second)
    defer ticker.Stop()

    for {
        status := checkService(service)
        statusChan <- status
        <-ticker.C
    }
}

func main() {
    config, err := loadConfig()
    if err != nil {
        log.Fatalf("Error loading config: %v", err)
    }

    statusManager := NewStatusManager()
    statusChan := make(chan ServiceStatus, len(config.Services))

    // Start monitoring each service
    for _, service := range config.Services {
        go monitorService(service, statusChan)
    }

    // Process status updates
    go func() {
        for status := range statusChan {
            statusManager.UpdateStatus(status)

            // Log status
            if !status.IsHealthy {
                log.Printf("⚠️ Service %s is DOWN! Status: %d, Error: %s, Response Time: %v\n",
                    status.Name, status.StatusCode, status.Error, status.ResponseTime)
            } else {
                log.Printf("✅ Service %s is UP (Status: %d, Response Time: %v)\n",
                    status.Name, status.StatusCode, status.ResponseTime)
            }
        }
    }()

    // Keep the main goroutine running
    select {}
}
