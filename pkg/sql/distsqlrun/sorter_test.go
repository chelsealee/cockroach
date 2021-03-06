// Copyright 2016 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package distsqlrun

import (
	"context"
	"fmt"
	math "math"
	"math/rand"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/base"
	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlbase"
	"github.com/cockroachdb/cockroach/pkg/storage/engine"
	"github.com/cockroachdb/cockroach/pkg/util/encoding"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/mon"
	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
)

func TestSorter(t *testing.T) {
	defer leaktest.AfterTest(t)()

	v := [6]sqlbase.EncDatum{}
	for i := range v {
		v[i] = intEncDatum(i)
	}

	asc := encoding.Ascending
	desc := encoding.Descending

	testCases := []struct {
		name     string
		spec     SorterSpec
		post     PostProcessSpec
		types    []sqlbase.ColumnType
		input    sqlbase.EncDatumRows
		expected sqlbase.EncDatumRows
	}{
		{
			name: "SortAll",
			// No specified input ordering and unspecified limit.
			spec: SorterSpec{
				OutputOrdering: convertToSpecOrdering(
					sqlbase.ColumnOrdering{
						{ColIdx: 0, Direction: asc},
						{ColIdx: 1, Direction: desc},
						{ColIdx: 2, Direction: asc},
					}),
			},
			types: threeIntCols,
			input: sqlbase.EncDatumRows{
				{v[1], v[0], v[4]},
				{v[3], v[4], v[1]},
				{v[4], v[4], v[4]},
				{v[3], v[2], v[0]},
				{v[4], v[4], v[5]},
				{v[3], v[3], v[0]},
				{v[0], v[0], v[0]},
			},
			expected: sqlbase.EncDatumRows{
				{v[0], v[0], v[0]},
				{v[1], v[0], v[4]},
				{v[3], v[4], v[1]},
				{v[3], v[3], v[0]},
				{v[3], v[2], v[0]},
				{v[4], v[4], v[4]},
				{v[4], v[4], v[5]},
			},
		}, {
			name: "SortLimit",
			// No specified input ordering but specified limit.
			spec: SorterSpec{
				OutputOrdering: convertToSpecOrdering(
					sqlbase.ColumnOrdering{
						{ColIdx: 0, Direction: asc},
						{ColIdx: 1, Direction: asc},
						{ColIdx: 2, Direction: asc},
					}),
			},
			post:  PostProcessSpec{Limit: 4},
			types: threeIntCols,
			input: sqlbase.EncDatumRows{
				{v[3], v[3], v[0]},
				{v[3], v[4], v[1]},
				{v[1], v[0], v[4]},
				{v[0], v[0], v[0]},
				{v[4], v[4], v[4]},
				{v[4], v[4], v[5]},
				{v[3], v[2], v[0]},
			},
			expected: sqlbase.EncDatumRows{
				{v[0], v[0], v[0]},
				{v[1], v[0], v[4]},
				{v[3], v[2], v[0]},
				{v[3], v[3], v[0]},
			},
		}, {
			name: "SortOffset",
			// No specified input ordering but specified offset and limit.
			spec: SorterSpec{
				OutputOrdering: convertToSpecOrdering(
					sqlbase.ColumnOrdering{
						{ColIdx: 0, Direction: asc},
						{ColIdx: 1, Direction: asc},
						{ColIdx: 2, Direction: asc},
					}),
			},
			post:  PostProcessSpec{Offset: 2, Limit: 2},
			types: threeIntCols,
			input: sqlbase.EncDatumRows{
				{v[3], v[3], v[0]},
				{v[3], v[4], v[1]},
				{v[1], v[0], v[4]},
				{v[0], v[0], v[0]},
				{v[4], v[4], v[4]},
				{v[4], v[4], v[5]},
				{v[3], v[2], v[0]},
			},
			expected: sqlbase.EncDatumRows{
				{v[3], v[2], v[0]},
				{v[3], v[3], v[0]},
			},
		}, {
			name: "SortFilterExpr",
			// No specified input ordering but specified postprocess filter expression.
			spec: SorterSpec{
				OutputOrdering: convertToSpecOrdering(
					sqlbase.ColumnOrdering{
						{ColIdx: 0, Direction: asc},
						{ColIdx: 1, Direction: asc},
						{ColIdx: 2, Direction: asc},
					}),
			},
			post:  PostProcessSpec{Filter: Expression{Expr: "@1 + @2 < 7"}},
			types: threeIntCols,
			input: sqlbase.EncDatumRows{
				{v[3], v[3], v[0]},
				{v[3], v[4], v[1]},
				{v[1], v[0], v[4]},
				{v[0], v[0], v[0]},
				{v[4], v[4], v[4]},
				{v[4], v[4], v[5]},
				{v[3], v[2], v[0]},
			},
			expected: sqlbase.EncDatumRows{
				{v[0], v[0], v[0]},
				{v[1], v[0], v[4]},
				{v[3], v[2], v[0]},
				{v[3], v[3], v[0]},
			},
		}, {
			name: "SortMatchOrderingNoLimit",
			// Specified match ordering length but no specified limit.
			spec: SorterSpec{
				OrderingMatchLen: 2,
				OutputOrdering: convertToSpecOrdering(
					sqlbase.ColumnOrdering{
						{ColIdx: 0, Direction: asc},
						{ColIdx: 1, Direction: asc},
						{ColIdx: 2, Direction: asc},
					}),
			},
			types: threeIntCols,
			input: sqlbase.EncDatumRows{
				{v[0], v[1], v[2]},
				{v[0], v[1], v[0]},
				{v[1], v[0], v[5]},
				{v[1], v[1], v[5]},
				{v[1], v[1], v[4]},
				{v[3], v[4], v[3]},
				{v[3], v[4], v[2]},
				{v[3], v[5], v[1]},
				{v[4], v[4], v[5]},
				{v[4], v[4], v[4]},
			},
			expected: sqlbase.EncDatumRows{
				{v[0], v[1], v[0]},
				{v[0], v[1], v[2]},
				{v[1], v[0], v[5]},
				{v[1], v[1], v[4]},
				{v[1], v[1], v[5]},
				{v[3], v[4], v[2]},
				{v[3], v[4], v[3]},
				{v[3], v[5], v[1]},
				{v[4], v[4], v[4]},
				{v[4], v[4], v[5]},
			},
		}, {
			name: "SortInputOrderingNoLimit",
			// Specified input ordering but no specified limit.
			spec: SorterSpec{
				OrderingMatchLen: 2,
				OutputOrdering: convertToSpecOrdering(
					sqlbase.ColumnOrdering{
						{ColIdx: 1, Direction: asc},
						{ColIdx: 2, Direction: asc},
						{ColIdx: 3, Direction: asc},
					}),
			},
			types: []sqlbase.ColumnType{intType, intType, intType, intType},
			input: sqlbase.EncDatumRows{
				{v[1], v[1], v[2], v[5]},
				{v[0], v[1], v[2], v[4]},
				{v[0], v[1], v[2], v[3]},
				{v[1], v[1], v[2], v[2]},
				{v[1], v[2], v[2], v[5]},
				{v[0], v[2], v[2], v[4]},
				{v[0], v[2], v[2], v[3]},
				{v[1], v[2], v[2], v[2]},
			},
			expected: sqlbase.EncDatumRows{
				{v[1], v[1], v[2], v[2]},
				{v[0], v[1], v[2], v[3]},
				{v[0], v[1], v[2], v[4]},
				{v[1], v[1], v[2], v[5]},
				{v[1], v[2], v[2], v[2]},
				{v[0], v[2], v[2], v[3]},
				{v[0], v[2], v[2], v[4]},
				{v[1], v[2], v[2], v[5]},
			},
		}, {
			name: "SortInputOrderingAlreadySorted",
			spec: SorterSpec{
				OrderingMatchLen: 2,
				OutputOrdering: convertToSpecOrdering(
					sqlbase.ColumnOrdering{
						{ColIdx: 1, Direction: asc},
						{ColIdx: 2, Direction: asc},
						{ColIdx: 3, Direction: asc},
					}),
			},
			types: []sqlbase.ColumnType{intType, intType, intType, intType},
			input: sqlbase.EncDatumRows{
				{v[1], v[1], v[2], v[2]},
				{v[0], v[1], v[2], v[3]},
				{v[0], v[1], v[2], v[4]},
				{v[1], v[1], v[2], v[5]},
				{v[1], v[2], v[2], v[2]},
				{v[0], v[2], v[2], v[3]},
				{v[0], v[2], v[2], v[4]},
				{v[1], v[2], v[2], v[5]},
			},
			expected: sqlbase.EncDatumRows{
				{v[1], v[1], v[2], v[2]},
				{v[0], v[1], v[2], v[3]},
				{v[0], v[1], v[2], v[4]},
				{v[1], v[1], v[2], v[5]},
				{v[1], v[2], v[2], v[2]},
				{v[0], v[2], v[2], v[3]},
				{v[0], v[2], v[2], v[4]},
				{v[1], v[2], v[2], v[5]},
			},
		},
	}

	ctx := context.Background()
	st := cluster.MakeTestingClusterSettings()
	tempEngine, err := engine.NewTempEngine(base.DefaultTestTempStorageConfig(st))
	if err != nil {
		t.Fatal(err)
	}
	defer tempEngine.Close()

	evalCtx := tree.MakeTestingEvalContext(st)
	defer evalCtx.Stop(ctx)
	diskMonitor := mon.MakeMonitor(
		"test-disk",
		mon.DiskResource,
		nil, /* curCount */
		nil, /* maxHist */
		-1,  /* increment: use default block size */
		math.MaxInt64,
		st,
	)
	diskMonitor.Start(ctx, nil /* pool */, mon.MakeStandaloneBudget(math.MaxInt64))
	defer diskMonitor.Stop(ctx)
	flowCtx := FlowCtx{
		EvalCtx:     evalCtx,
		Settings:    cluster.MakeTestingClusterSettings(),
		TempStorage: tempEngine,
		diskMonitor: &diskMonitor,
	}

	for _, c := range testCases {
		// Test with several memory limits:
		// 0: Use the default limit.
		// 1: Immediately switch to disk.
		// 1150: This is the memory used after we store a couple of rows in
		// memory. Tests the transfer of rows from memory to disk on
		// initialization.
		// 2048: A memory limit that should not be hit; the processor will not
		// use disk.
		for _, memLimit := range []int64{0, 1, 1150, 2048} {
			// In theory, SortAllProcessor should be able to handle all sorting
			// strategies, as the other processors are optimizations.
			for _, testingForceSortAll := range []bool{false, true} {
				t.Run(fmt.Sprintf("MemLimit=%d", memLimit), func(t *testing.T) {
					in := NewRowBuffer(c.types, c.input, RowBufferArgs{})
					out := &RowBuffer{}

					var s Processor
					if !testingForceSortAll {
						var err error
						s, err = newSorter(context.Background(), &flowCtx, &c.spec, in, &c.post, out)
						if err != nil {
							t.Fatal(err)
						}
					} else {
						var err error
						s, err = newSortAllProcessor(context.Background(), &flowCtx, &c.spec, in, &c.post, out)
						if err != nil {
							t.Fatal(err)
						}
					}
					// Override the default memory limit. This will result in using
					// a memory row container which will hit this limit and fall
					// back to using a disk row container.
					flowCtx.testingKnobs.MemoryLimitBytes = memLimit
					s.Run(context.Background(), nil /* wg */)
					if !out.ProducerClosed {
						t.Fatalf("output RowReceiver not closed")
					}

					var retRows sqlbase.EncDatumRows
					for {
						row := out.NextNoMeta(t)
						if row == nil {
							break
						}
						retRows = append(retRows, row)
					}

					expStr := c.expected.String(c.types)
					retStr := retRows.String(c.types)
					if expStr != retStr {
						t.Errorf("invalid results; expected:\n   %s\ngot:\n   %s",
							expStr, retStr)
					}
				})
			}
		}
	}
}

var twoColOrdering = convertToSpecOrdering(sqlbase.ColumnOrdering{
	{ColIdx: 0, Direction: encoding.Ascending},
	{ColIdx: 1, Direction: encoding.Ascending},
})

// BenchmarkSortAll times how long it takes to sort an input of varying length.
func BenchmarkSortAll(b *testing.B) {
	const numCols = 2

	ctx := context.Background()
	st := cluster.MakeTestingClusterSettings()
	evalCtx := tree.MakeTestingEvalContext(st)
	defer evalCtx.Stop(ctx)
	flowCtx := FlowCtx{
		Settings: st,
		EvalCtx:  evalCtx,
	}

	rng := rand.New(rand.NewSource(timeutil.Now().UnixNano()))
	spec := SorterSpec{OutputOrdering: twoColOrdering}
	post := PostProcessSpec{}

	for _, numRows := range []int{1 << 4, 1 << 8, 1 << 12, 1 << 16} {
		b.Run(fmt.Sprintf("rows=%d", numRows), func(b *testing.B) {
			input := NewRepeatableRowSource(twoIntCols, makeRandIntRows(rng, numRows, numCols))
			b.SetBytes(int64(numRows * numCols * 8))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s, err := newSorter(context.Background(), &flowCtx, &spec, input, &post, &RowDisposer{})
				if err != nil {
					b.Fatal(err)
				}
				s.Run(context.Background(), nil /* wg */)
				input.Reset()
			}
		})
	}
}

// BenchmarkSortLimit times how long it takes to sort a fixed size input with
// varying limits.
func BenchmarkSortLimit(b *testing.B) {
	const numCols = 2

	ctx := context.Background()
	st := cluster.MakeTestingClusterSettings()
	evalCtx := tree.MakeTestingEvalContext(st)
	defer evalCtx.Stop(ctx)
	flowCtx := FlowCtx{
		Settings: st,
		EvalCtx:  evalCtx,
	}

	rng := rand.New(rand.NewSource(timeutil.Now().UnixNano()))
	spec := SorterSpec{OutputOrdering: twoColOrdering}

	const numRows = 1 << 16
	b.Run(fmt.Sprintf("rows=%d", numRows), func(b *testing.B) {
		input := NewRepeatableRowSource(twoIntCols, makeRandIntRows(rng, numRows, numCols))
		for _, limit := range []uint64{1 << 4, 1 << 8, 1 << 12, 1 << 16} {
			post := PostProcessSpec{Limit: limit}
			b.Run(fmt.Sprintf("Limit=%d", limit), func(b *testing.B) {
				b.SetBytes(int64(numRows * numCols * 8))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					s, err := newSorter(context.Background(), &flowCtx, &spec, input, &post, &RowDisposer{})
					if err != nil {
						b.Fatal(err)
					}
					s.Run(context.Background(), nil /* wg */)
					input.Reset()
				}
			})

		}
	})
}

// BenchmarkSortChunks times how long it takes to sort an input which is already
// sorted on a prefix.
func BenchmarkSortChunks(b *testing.B) {
	const numCols = 2

	ctx := context.Background()
	st := cluster.MakeTestingClusterSettings()
	evalCtx := tree.MakeTestingEvalContext(st)
	defer evalCtx.Stop(ctx)
	flowCtx := FlowCtx{
		Settings: st,
		EvalCtx:  evalCtx,
	}

	rng := rand.New(rand.NewSource(timeutil.Now().UnixNano()))
	spec := SorterSpec{
		OutputOrdering:   twoColOrdering,
		OrderingMatchLen: 1,
	}
	post := PostProcessSpec{}

	for _, numRows := range []int{1 << 4, 1 << 8, 1 << 12, 1 << 16} {
		for chunkSize := 1; chunkSize <= numRows; chunkSize *= 4 {
			b.Run(fmt.Sprintf("rows=%d,ChunkSize=%d", numRows, chunkSize), func(b *testing.B) {
				rows := makeRandIntRows(rng, numRows, numCols)
				for i, row := range rows {
					row[0] = intEncDatum(i / chunkSize)
				}
				input := NewRepeatableRowSource(twoIntCols, rows)
				b.SetBytes(int64(numRows * numCols * 8))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					s, err := newSorter(context.Background(), &flowCtx, &spec, input, &post, &RowDisposer{})
					if err != nil {
						b.Fatal(err)
					}
					s.Run(context.Background(), nil /* wg */)
					input.Reset()
				}
			})
		}
	}
}
