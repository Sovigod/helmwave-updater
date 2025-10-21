package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/cli"
)

// readHelmwave reads and unmarshals helmwave YAML file into structures.
func readHelmwave(filename string) ([]byte, Helmwave, error) {
	vlog("reading input file: %s", filename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, Helmwave{}, err
	}
	vlog("read %d bytes from %s", len(data), filename)
	// Preprocess: remove `repositories:` section from the raw YAML text before unmarshalling.
	// The file may contain templating expressions (e.g. {{ env "..." }}) which break strict YAML parsing.
	// We must NOT modify the on-disk file; instead, strip the repositories block only from the in-memory bytes
	// used for YAML unmarshalling.
	// remove repositories and registries sections from in-memory text before parsing
	processed := removeTopLevelSection(data, "repositories")
	processed = removeTopLevelSection(processed, "registries")

	var hw Helmwave
	if err := yaml.Unmarshal(processed, &hw); err != nil {
		return nil, Helmwave{}, err
	}
	return data, hw, nil
}

// removeTopLevelSection removes a top-level YAML section (including its indented block)
// by name from the provided byte slice and returns the processed bytes.
// It is a conservative line-based stripper: it finds the line that starts with the
// section key followed by ':' and removes that line and all following lines that are
// indented (have greater indent) until a line with indent <= sectionIndent is found.
func removeTopLevelSection(input []byte, section string) []byte {
	text := string(input)
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))

	skip := false
	sectionIndent := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)

		if !skip {
			// detect top-level section line like "repositories:" possibly with leading/trailing spaces
			if strings.HasPrefix(strings.TrimSpace(line), section+":") {
				skip = true
				sectionIndent = indent
				// skip this line (do not append)
				continue
			}
			out = append(out, line)
		} else {
			// currently skipping: continue skipping while indent > sectionIndent
			if strings.TrimSpace(line) == "" {
				// preserve empty lines inside skipped block (still skip them)
				continue
			}
			if indent > sectionIndent {
				// still inside the section block -> skip
				continue
			}
			// reached a line that is at same or less indent -> stop skipping and include this line
			skip = false
			out = append(out, line)
		}
	}

	return []byte(strings.Join(out, "\n"))
}

// updateFileText returns edited file content (string) with versions replaced according to versionMap.
func updateFileText(original []byte, versionMap map[string]string, chartVersionMap map[string]string) string {
	text := string(original)
	lines := strings.Split(text, "\n")

	for relName, newVer := range versionMap {
		vlog("will update release %s -> %s in file text", relName, newVer)
		inRelease := false
		inChart := false
		var chartIndent int

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			trimmed := strings.TrimSpace(line)
			indent := len(line) - len(strings.TrimLeft(line, " "))

			if strings.HasPrefix(trimmed, "- name:") {
				namePart := strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
				if idx := strings.Index(namePart, "#"); idx >= 0 {
					namePart = strings.TrimSpace(namePart[:idx])
				}
				namePart = strings.Trim(namePart, "'\"")
				if namePart == relName {
					inRelease = true
					inChart = false
					continue
				}
				if inRelease {
					inRelease = false
					inChart = false
				}
			}

			if !inRelease {
				continue
			}

			if strings.HasPrefix(trimmed, "chart:") {
				if strings.TrimSpace(trimmed) == "chart:" {
					inChart = true
					chartIndent = indent
					continue
				}
			}

			if inChart {
				if indent <= chartIndent && !strings.HasPrefix(trimmed, "version:") {
					inChart = false
					continue
				}

				if strings.HasPrefix(trimmed, "version:") {
					after := strings.TrimSpace(strings.TrimPrefix(trimmed, "version:"))
					comment := ""
					if idx := strings.Index(after, "#"); idx >= 0 {
						comment = " " + strings.TrimSpace(after[idx:])
					}
					origVal := strings.TrimSpace(after)
					origVal = strings.TrimRight(origVal, "# ")
					origVal = strings.Trim(origVal, "'\"")

					if origVal == newVer {
						vlog("existing version for release %s equals target %s; skipping file edit", relName, newVer)
						inChart = false
						inRelease = false
						// continue scanning for other occurrences of the same release later in the file
						continue
					}
					useQuotes := strings.Contains(after, "\"") || strings.Contains(after, "'")
					var valStr string
					if useQuotes {
						valStr = fmt.Sprintf("\"%s\"", newVer)
					} else {
						valStr = newVer
					}
					newLine := strings.Repeat(" ", indent) + "version: " + valStr + comment
					vlog("replacing line %d for release %s: %q -> %q", i+1, relName, lines[i], newLine)
					lines[i] = newLine
					inChart = false
					inRelease = false
					// continue scanning to update possible additional occurrences of the same release
					continue
				}
			}
		}
	}

	// Second pass: update top-level anchors (for example ".options: &options") that contain a chart: block
	// We look for top-level keys that start with '.' (like .options) and inside their chart block
	// try to match chart.name and update chart.version according to chartVersionMap.
	for chartFullName, newVer := range chartVersionMap {
		inAnchor := false
		inChart := false
		var anchorIndent int
		var foundChartName string

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			trimmed := strings.TrimSpace(line)
			indent := len(line) - len(strings.TrimLeft(line, " "))

			// detect top-level anchor like ".options: &options" or ".options:"
			if !inAnchor && strings.HasPrefix(trimmed, ".") && strings.Contains(trimmed, ":") {
				inAnchor = true
				anchorIndent = indent
				inChart = false
				foundChartName = ""
				continue
			}

			if inAnchor {
				// if we hit another top-level key (same or smaller indent) that is not part of chart, exit anchor
				if indent <= anchorIndent && !strings.HasPrefix(trimmed, "chart:") && !strings.HasPrefix(trimmed, "#") {
					inAnchor = false
					inChart = false
					foundChartName = ""
					continue
				}

				if strings.HasPrefix(trimmed, "chart:") {
					if strings.TrimSpace(trimmed) == "chart:" {
						inChart = true
						// chartIndent equals current indent
						// continue to next lines to find name/version
						continue
					}
				}

				if inChart {
					// if we left chart block
					if indent <= anchorIndent && !strings.HasPrefix(trimmed, "name:") && !strings.HasPrefix(trimmed, "version:") {
						inChart = false
						continue
					}

					if strings.HasPrefix(trimmed, "name:") {
						nameVal := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
						nameVal = strings.Trim(nameVal, "'\"")
						// store found chart name to later compare when we see version
						foundChartName = nameVal
						continue
					}

					if strings.HasPrefix(trimmed, "version:") {
						if foundChartName == chartFullName {
							after := strings.TrimSpace(strings.TrimPrefix(trimmed, "version:"))
							comment := ""
							if idx := strings.Index(after, "#"); idx >= 0 {
								comment = " " + strings.TrimSpace(after[idx:])
							}
							origVal := strings.TrimSpace(after)
							origVal = strings.TrimRight(origVal, "# ")
							origVal = strings.Trim(origVal, "'\"")

							if origVal == newVer {
								// already up-to-date
								inChart = false
								inAnchor = false
								foundChartName = ""
								continue
							}
							useQuotes := strings.Contains(after, "\"") || strings.Contains(after, "'")
							var valStr string
							if useQuotes {
								valStr = fmt.Sprintf("\"%s\"", newVer)
							} else {
								valStr = newVer
							}
							newLine := strings.Repeat(" ", indent) + "version: " + valStr + comment
							vlog("replacing anchor line %d for chart %s: %q -> %q", i+1, chartFullName, lines[i], newLine)
							lines[i] = newLine
							inChart = false
							inAnchor = false
							foundChartName = ""
							continue
						}
					}
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

// writeOutput writes content to outFile and logs result.
func writeOutput(outFile, out string) error {
	if err := os.WriteFile(outFile, []byte(out), 0644); err != nil {
		return err
	}
	vlog("wrote %d bytes to %s", len(out), outFile)
	log.Printf("Wrote updated file: %s", outFile)
	return nil
}
func main() {
	// allow filename via flag or positional argument
	flag.BoolVar(&showVersion, "version", false, "print latest release version from GitHub and exit")
	flag.StringVar(&filename, "file", "helmwave.yml.tpl", "path to helmwave yaml file")
	flag.BoolVar(&inplace, "inplace", false, "modify the original file instead of creating a .updated copy")
	flag.BoolVar(&verbose, "verbose", false, "enable verbose logging")
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		return
	}

	settings := cli.New()

	vlog("starting: file=%s inplace=%v verbose=%v", filename, inplace, verbose)
	vlog("helm settings: repo config=%s repo cache=%s namespace=%s", settings.RepositoryConfig, settings.RepositoryCache, settings.Namespace())

	indexes, err := loadIndexes(settings)
	if err != nil {
		log.Fatalf("failed to load repo file: %v", err)
	}

	data, hw, err := readHelmwave(filename)
	if err != nil {
		log.Fatalf("failed to read helmwave: %v", err)
	}

	processReleases(&hw, indexes)

	versionMap := buildVersionMap(&hw)
	chartVersionMap := buildChartVersionMap(&hw)

	out := updateFileText(data, versionMap, chartVersionMap)

	outFile := filename + ".updated"
	if inplace {
		outFile = filename
	}
	if err := writeOutput(outFile, out); err != nil {
		log.Fatalf("failed to write %s: %v", outFile, err)
	}
}
