package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRegisterEVs(t *testing.T) {
	vehicleRepo := &InMemoryVehicleRepository{}
	groupRepo := &InMemoryGroupRepository{groupToCar: make(map[int]int)}
	vehicleService := &VehicleService{vehicleRepo: vehicleRepo}
	eventBus := NewEventBus()
	vehicleService.eventBus = eventBus

	r := setupRouter(vehicleRepo, groupRepo, vehicleService, eventBus)

	payload := `[{"id":1,"seats":4},{"id":2,"seats":6}]`
	req, _ := http.NewRequest("PUT", "/evs", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "EVs registered successfully")
	assert.Len(t, vehicleRepo.GetAllVehicles(), 2)
}

func TestJourneyRequest(t *testing.T) {
	vehicleRepo := &InMemoryVehicleRepository{
		vehicles: []Vehicle{{ID: 1, Seats: 4}},
	}
	groupRepo := &InMemoryGroupRepository{groupToCar: make(map[int]int)}
	eventBus := NewEventBus()
	vehicleService := &VehicleService{vehicleRepo: vehicleRepo, eventBus: eventBus}

	r := setupRouter(vehicleRepo, groupRepo, vehicleService, eventBus)

	payload := `{"id":1,"people":3}`
	req, _ := http.NewRequest("POST", "/journey", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "Journey started")
	assert.Equal(t, 1, groupRepo.groupToCar[1])
}

func TestDropOffGroup(t *testing.T) {
	vehicleRepo := &InMemoryVehicleRepository{
		vehicles: []Vehicle{{ID: 1, Seats: 4}},
	}
	groupRepo := &InMemoryGroupRepository{
		groupsQueue: []Group{{ID: 1, People: 3}},
		groupToCar:  map[int]int{1: 1},
	}
	eventBus := NewEventBus()
	vehicleService := &VehicleService{vehicleRepo: vehicleRepo, eventBus: eventBus}

	r := setupRouter(vehicleRepo, groupRepo, vehicleService, eventBus)

	payload := `{"id":1}`
	req, _ := http.NewRequest("POST", "/dropoff", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "Group dropped off")
}

func TestLocateGroup(t *testing.T) {
	vehicleRepo := &InMemoryVehicleRepository{
		vehicles: []Vehicle{{ID: 1, Seats: 4}},
	}
	groupRepo := &InMemoryGroupRepository{
		groupToCar: map[int]int{1: 1},
	}
	eventBus := NewEventBus()
	vehicleService := &VehicleService{vehicleRepo: vehicleRepo, eventBus: eventBus}

	r := setupRouter(vehicleRepo, groupRepo, vehicleService, eventBus)

	payload := `{"id":1}`
	req, _ := http.NewRequest("POST", "/locate", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"car_id":1`)
}

func setupRouter(vehicleRepo *InMemoryVehicleRepository, groupRepo *InMemoryGroupRepository, vehicleService *VehicleService, eventBus *EventBus) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	eventBus.Register("VehicleAssigned", func(e Event) {
		data, ok := e.Data.(map[string]interface{})
		if !ok {
			log.Println("Invalid event data format")
			return
		}
		groupID, groupOk := data["group_id"].(int)
		vehicleID, vehicleOk := data["vehicle_id"].(int)
		if !groupOk || !vehicleOk {
			log.Printf("Missing event data: group_id=%v, vehicle_id=%v", data["group_id"], data["vehicle_id"])
			return
		}
		log.Printf("Group %d assigned to Vehicle %d\n", groupID, vehicleID)
	})
	r.PUT("/evs", func(c *gin.Context) {
		var newVehicles []Vehicle
		if err := c.ShouldBindJSON(&newVehicles); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
			return
		}
		vehicleRepo.SaveVehicles(newVehicles)
		groupRepo.groupsQueue = []Group{}
		groupRepo.groupToCar = make(map[int]int)
		c.JSON(http.StatusOK, gin.H{"message": "EVs registered successfully"})
	})

	r.POST("/journey", func(c *gin.Context) {
		var group Group
		if err := c.ShouldBindJSON(&group); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
			return
		}
		if carID, assigned := vehicleService.AssignVehicleToGroup(group); assigned {
			groupRepo.groupToCar[group.ID] = carID
			eventBus.Emit(Event{
				Type: "VehicleAssigned",
				Data: map[string]interface{}{
					"group_id": group.ID,
					"car_id":   carID,
				},
			})

			c.JSON(http.StatusOK, gin.H{"message": "Journey started", "car_id": carID})
			return
		}
		groupRepo.AddGroup(group)
		c.JSON(http.StatusAccepted, gin.H{"message": "Added to waitlist"})
	})

	r.POST("/dropoff", func(c *gin.Context) {
		var request struct {
			ID int `json:"id"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
			return
		}
		if carID, exists := groupRepo.groupToCar[request.ID]; exists {
			group, _ := groupRepo.FindGroup(request.ID)
			vehicleService.ReleaseVehicleSeats(carID, group.People)
			delete(groupRepo.groupToCar, request.ID)
			c.JSON(http.StatusOK, gin.H{"message": "Group dropped off"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
	})

	r.POST("/locate", func(c *gin.Context) {
		var request struct {
			ID int `json:"id"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
			return
		}
		if carID, exists := groupRepo.groupToCar[request.ID]; exists {
			c.JSON(http.StatusOK, gin.H{"car_id": carID})
			return
		}
		c.JSON(http.StatusNoContent, nil)
	})

	return r
}
