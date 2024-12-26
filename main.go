package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Vehicle struct {
	ID    int `json:"id"`
	Seats int `json:"seats"`
}

type Group struct {
	ID     int `json:"id"`
	People int `json:"people"`
}

type VehicleRepository interface {
	GetAllVehicles() []Vehicle
	SaveVehicles(vehicles []Vehicle)
	UpdateVehicleSeats(vehicleID, seats int)
}

type GroupRepository interface {
	AddGroup(group Group)
	RemoveGroup(groupID int) bool
	FindGroup(groupID int) (Group, bool)
	GetNextWaitingGroup() (Group, bool)
}

type VehicleService struct {
	vehicleRepo VehicleRepository
	eventBus    *EventBus
}
type Event struct {
	Type string
	Data interface{}
}

type EventBus struct {
	listeners map[string][]func(Event)
}

func NewEventBus() *EventBus {
	return &EventBus{
		listeners: make(map[string][]func(Event)),
	}
}

func (eb *EventBus) Register(eventType string, listener func(Event)) {
	eb.listeners[eventType] = append(eb.listeners[eventType], listener)
}
func (eb *EventBus) Emit(event Event) {
	if handlers, found := eb.listeners[event.Type]; found {
		for _, handler := range handlers {
			go handler(event)
		}
	}
}

func (vs *VehicleService) AssignVehicleToGroup(group Group) (int, bool) {
	vehicles := vs.vehicleRepo.GetAllVehicles()
	for i := range vehicles {
		if vehicles[i].Seats >= group.People {
			vs.vehicleRepo.UpdateVehicleSeats(vehicles[i].ID, vehicles[i].Seats-group.People)
			vs.eventBus.Emit(Event{
				Type: "VehicleAssigned",
				Data: map[string]interface{}{
					"group_id":   group.ID,
					"vehicle_id": vehicles[i].ID,
				},
			})
			return vehicles[i].ID, true
		}
	}
	return 0, false
}

func (vs *VehicleService) ReleaseVehicleSeats(vehicleID, seats int) {
	vs.vehicleRepo.UpdateVehicleSeats(vehicleID, seats)
}

type InMemoryVehicleRepository struct {
	vehicles []Vehicle
}

func (repo *InMemoryVehicleRepository) GetAllVehicles() []Vehicle {
	return repo.vehicles
}

func (repo *InMemoryVehicleRepository) SaveVehicles(vehicles []Vehicle) {
	repo.vehicles = vehicles
}

func (repo *InMemoryVehicleRepository) UpdateVehicleSeats(vehicleID, seats int) {
	for i := range repo.vehicles {
		if repo.vehicles[i].ID == vehicleID {
			repo.vehicles[i].Seats = seats
			break
		}
	}
}

type InMemoryGroupRepository struct {
	groupsQueue []Group
	groupToCar  map[int]int
}

func (repo *InMemoryGroupRepository) AddGroup(group Group) {
	repo.groupsQueue = append(repo.groupsQueue, group)
}

func (repo *InMemoryGroupRepository) RemoveGroup(groupID int) bool {
	for i, group := range repo.groupsQueue {
		if group.ID == groupID {
			repo.groupsQueue = append(repo.groupsQueue[:i], repo.groupsQueue[i+1:]...)
			return true
		}
	}
	delete(repo.groupToCar, groupID)
	return false
}

func (repo *InMemoryGroupRepository) FindGroup(groupID int) (Group, bool) {
	for _, group := range repo.groupsQueue {
		if group.ID == groupID {
			return group, true
		}
	}
	return Group{}, false
}

func (repo *InMemoryGroupRepository) GetNextWaitingGroup() (Group, bool) {
	if len(repo.groupsQueue) > 0 {
		return repo.groupsQueue[0], true
	}
	return Group{}, false
}

func main() {
	eventBus := NewEventBus()
	vehicleRepo := &InMemoryVehicleRepository{}
	groupRepo := &InMemoryGroupRepository{groupToCar: make(map[int]int)}
	vehicleService := &VehicleService{vehicleRepo: vehicleRepo, eventBus: eventBus}
	// log when a group is successfully assigned to a vehicle, this include logging, but is only for the
	// challenge in a real situation this can sending an email, SMS, or other notifications
	eventBus.Register("VehicleAssigned", func(e Event) {
		data := e.Data.(map[string]interface{})
		groupID := data["group_id"].(int)
		vehicleID := data["vehicle_id"].(int)
		log.Printf("Group %d assigned to Vehicle %d\n", groupID, vehicleID)
	})
	// one event handler here to demonstrate the idea, more can be added as needed in a real situation
	r := gin.Default()
	// health check endpoint to verify the server is ready
	r.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	// endpoint to register electric vehicles
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
	// endpoint to handle journey requests
	r.POST("/journey", func(c *gin.Context) {
		var group Group
		if err := c.ShouldBindJSON(&group); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
			return
		}
		if carID, assigned := vehicleService.AssignVehicleToGroup(group); assigned {
			groupRepo.groupToCar[group.ID] = carID
			c.JSON(http.StatusOK, gin.H{"message": "Journey started", "car_id": carID})
			return
		}
		groupRepo.AddGroup(group)
		c.JSON(http.StatusAccepted, gin.H{"message": "Added to waitlist"})
	})
	// endpoint to drop off a group and release vehicle seats
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

	r.Run(":80")
}
