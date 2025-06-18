package store

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	DB *sql.DB
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	schema := `
    CREATE TABLE IF NOT EXISTS transactions (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        symbol TEXT,
        side TEXT,
        amount REAL,
        price REAL,
        time TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`
	_, err = db.Exec(schema)
	if err != nil {
		return nil, err
	}

	return &Store{DB: db}, nil
}

func (s *Store) LogTransaction(symbol, side string, amount, price float64) error {
	_, err := s.DB.Exec("INSERT INTO transactions (symbol, side, amount, price) VALUES (?, ?, ?, ?)",
		symbol, side, amount, price)
	return err
}
