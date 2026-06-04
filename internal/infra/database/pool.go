// pool.go — pgxpool helper,统一加 OTel pgx tracer。
//
// 用法(取代 pgxpool.New(...)):
//
//	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
//
// 自动给每个 SQL query 加 child span,ARMS 拓扑图把 RDS 显示成下游节点,
// trace UI 能看到 SQL duration / db.statement / db.operation。
package database

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wanmuchengchuan/listen/internal/logext"
)

// NewPool 建 pgxpool + otelpgx tracer。listen 子命令 / server 都用这个。
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	const where = "database.NewPool"
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		logext.Errorf(ctx, "[%s] parse url failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] new pool failed,err:%+v", where, err.Error())
		return nil, err
	}
	return pool, nil
}
