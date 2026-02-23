#!/bin/bash
set -e

FIXTURE_DIR="/inference/INGEST_PROCESSING/019c244c-b684-7a2d-a6e4-ed0bb2930282/pdf_to_images"
DATA_DIR="/tmp/test_gpu_feeder_v2"
DB_PATH="$DATA_DIR/jobs.db"

echo "🧪 GPU Feeder v2 Test - vLLM run-batch (440 Images PNG)"
echo "========================================================="

# 1. Cleanup précédent
if [ -d "$DATA_DIR" ]; then
    echo "🧹 Cleaning up previous test..."
    rm -rf "$DATA_DIR"
fi

# 2. Setup
echo "📁 Setting up test directory..."
mkdir -p "$DATA_DIR/stage_vision/pending"
mkdir -p "$DATA_DIR/stage_vision/done"
mkdir -p "$DATA_DIR"

# 3. Vérifier fixture
if [ ! -d "$FIXTURE_DIR" ]; then
    echo "❌ Fixture not found: $FIXTURE_DIR"
    exit 1
fi

TOTAL_IMAGES=$(ls -1 "$FIXTURE_DIR"/*.png 2>/dev/null | wc -l)
echo "✓ Fixture contains $TOTAL_IMAGES images"

# 4. Init DB
echo "🗄️  Initializing SQLite database..."
sqlite3 "$DB_PATH" < /inference/horos47/services/gpufeeder/schema.sql

# 5. Générer parent UUID
PARENT_UUID=$(python3 -c "import uuid; print(uuid.uuid4())")
echo "   Parent job UUID: $PARENT_UUID"

# 6. Créer jobs depuis fixture
echo "📝 Creating $TOTAL_IMAGES jobs from fixture images..."

IDX=1
for IMG in "$FIXTURE_DIR"/*.png; do
    if [ -f "$IMG" ]; then
        # Générer UUID v7 (approximation avec python)
        JOB_UUID=$(python3 -c "import uuid; print(uuid.uuid4())")
        BASENAME=$(basename "$IMG")

        # Copier image vers pending/ avec UUID comme nom
        DEST_PATH="$DATA_DIR/stage_vision/pending/$JOB_UUID.png"
        cp "$IMG" "$DEST_PATH"

        # SHA256 du payload JSON (path + index) pour idempotence
        PAYLOAD_JSON="{\"path\":\"$DEST_PATH\",\"fragment\":$IDX}"
        SHA256=$(echo -n "$PAYLOAD_JSON" | sha256sum | cut -d' ' -f1)

        # Convertir UUIDs en bytes hex pour SQLite BLOB
        JOB_BYTES=$(python3 -c "import uuid; print(uuid.UUID('$JOB_UUID').bytes.hex())")
        PARENT_BYTES=$(python3 -c "import uuid; print(uuid.UUID('$PARENT_UUID').bytes.hex())")

        # Insert job
        sqlite3 "$DB_PATH" <<EOF
INSERT INTO gpu_jobs (
    id, payload_sha256, parent_id, fragment_index, total_fragments,
    model_type, payload_path, created_at
) VALUES (
    X'$JOB_BYTES',
    '$SHA256',
    X'$PARENT_BYTES',
    $IDX,
    $TOTAL_IMAGES,
    'vision',
    '$DEST_PATH',
    $(date +%s)
);
EOF

        if [ $((IDX % 50)) -eq 0 ]; then
            echo "   Created $IDX/$TOTAL_IMAGES jobs..."
        fi

        ((IDX++))
    fi
done

echo "   ✓ Created $((IDX-1)) jobs"

# 7. Vérifier DB
echo ""
echo "📊 Database status:"
sqlite3 "$DB_PATH" "SELECT model_type, status, COUNT(*) FROM gpu_jobs GROUP BY model_type, status;"

echo ""
echo "📂 Pending images:"
ls "$DATA_DIR/stage_vision/pending/" | wc -l

echo ""
echo "✅ Test setup complete!"
echo ""
echo "🚀 To run GPU Feeder:"
echo "   cd /inference/horos47"
echo "   ./bin/gpu_feeder_v2 -db $DB_PATH -datadir $DATA_DIR"
echo ""
echo "⚠️  NOTE: vLLM must be installed and 'vllm run-batch' command available"
echo ""
echo "📈 Monitor progress:"
echo "   watch -n 2 'sqlite3 $DB_PATH \"SELECT status, COUNT(*) FROM gpu_jobs GROUP BY status\"'"
echo ""
echo "🔍 Check results after completion:"
echo "   ls $DATA_DIR/stage_vision/done/ | wc -l"
echo "   sqlite3 $DB_PATH \"SELECT * FROM v_stats\""
