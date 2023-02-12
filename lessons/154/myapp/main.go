package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	_ "github.com/go-sql-driver/mysql"
)

var (
	maxClients = flag.Int("maxClients", 100, "Maximum number of virtual clients")
)

type metrics struct {
	duration *prometheus.SummaryVec
}

func NewMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		duration: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Namespace:  "tester",
			Name:       "duration_seconds",
			Help:       "Duration of the request.",
			Objectives: map[float64]float64{0.9: 0.01, 0.99: 0.001},
		}, []string{"db", "operation"}),
	}
	reg.MustRegister(m.duration)
	return m
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {
	// Parse the command line into the defined flags
	flag.Parse()

	// Create Prometheus registry
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Create Prometheus HTTP server to expose metrics
	pMux := http.NewServeMux()
	promHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	pMux.Handle("/metrics", promHandler)

	go func() {
		log.Fatal(http.ListenAndServe(":8081", pMux))
	}()

	dbUrl := "postgres://myapp:devops123@192.168.50.222:5432/benchmarks"
	dbpool, err := pgxpool.New(context.Background(), dbUrl)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer dbpool.Close()

	db, err := sql.Open("mysql", "myappv2:devops123@tcp(192.168.50.87:3306)/benchmarks")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// See "Important settings" section.
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	_, err = db.Exec("INSERT INTO authors(first_name,last_name) VALUES(?,?)", "asd", "sfb")
	if err != nil {
		log.Fatalf("db.Exec failed: %v", err)
	}

	for i := 0; i < *maxClients; i++ {
		firstName, lastName := genName()

		// Get timestamp for histogram
		now := time.Now()

		err = insertAuthorToPostgres(dbpool, firstName, lastName)
		if err != nil {
			log.Fatalf("insertAuthor failed: %v", err)
		}

		// Record request duration
		m.duration.With(prometheus.Labels{"db": "postgres", "operation": "write"}).Observe(time.Since(now).Seconds())

		now = time.Now()

		_, err = db.Exec("INSERT INTO authors(first_name,last_name) VALUES(?,?)", firstName, lastName)
		if err != nil {
			log.Fatalf("db.Exec failed: %v", err)
		}

		// Record request duration
		m.duration.With(prometheus.Labels{"db": "mysql", "operation": "write"}).Observe(time.Since(now).Seconds())

		time.Sleep(1 * time.Second)
	}

	fmt.Println("finished")
	select {}
}

func insertAuthorToPostgres(p *pgxpool.Pool, firstName string, lastName string) error {
	_, err := p.Exec(context.Background(), "INSERT INTO authors(first_name,last_name) VALUES($1,$2)", firstName, lastName)
	return err
}

func genName() (string, string) {
	caser := cases.Title(language.English)

	firstName := caser.String(petname.Generate(1, ""))
	lastName := caser.String(petname.Generate(1, ""))

	return firstName, lastName
}
