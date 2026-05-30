package chat

import (
	"database/sql"
	"fmt"
)

func changed(result sql.Result) (bool, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read rows affected: %w", err)
	}
	return affected > 0, nil
}
