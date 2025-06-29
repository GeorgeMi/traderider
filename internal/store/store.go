package store

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"time"
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
	_, err := s.DB.Exec(`
        INSERT INTO transactions (symbol, side, amount, price, time) 
        VALUES (?, ?, ?, ?, ?)
    `, symbol, side, amount, price, time.Now())
	return err
}

func (s *Store) GetLastBuyTransaction(symbol string) (float64, float64, error) {
	row := s.DB.QueryRow(`
        SELECT amount, price 
        FROM transactions 
        WHERE symbol = ? AND side = 'BUY' 
        ORDER BY time DESC 
        LIMIT 1
    `, symbol)

	var amount, price float64
	err := row.Scan(&amount, &price)
	if err != nil {
		return 0, 0, err
	}
	return amount, price, nil
}

func (s *Store) GetTransactions(symbol string, limit int) ([]Transaction, error) {
	rows, err := s.DB.Query(`
        SELECT side, amount, price, time
        FROM transactions
        WHERE symbol = ?
        ORDER BY time DESC
        LIMIT ?
    `, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Transaction
	for rows.Next() {
		var t Transaction
		err := rows.Scan(&t.Side, &t.Amount, &t.Price, &t.Time)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, nil
}

type Transaction struct {
	Side   string
	Amount float64
	Price  float64
	Time   time.Time
}
