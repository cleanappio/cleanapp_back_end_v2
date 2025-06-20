# Create a combined file with all code files
echo "// Combined Project Code" > combined-project.txt
echo "" >> combined-project.txt
find . -name "*.go" -o -name "*.toml" -o -name ".env.example" -o -name "go.*" -o -name "Makefile" -o -name "*.md" | \
grep -v node_modules | \
while read file; do
    echo "// File: $file" >> combined-project.txt
    echo "" >> combined-project.txt
    cat "$file" >> combined-project.txt
    echo "" >> combined-project.txt
    echo "// ----------------" >> combined-project.txt
    echo "" >> combined-project.txt
done
