package posthog

import (
	"regexp"
	"strconv"
	"strings"
)

// The three regexes and the parsing logic below mirror PostHog's own
// chain_to_elements (posthog/models/element/element.py). The REST /events/
// endpoint returned a parsed elements array derived from elements_chain; the
// query endpoint only exposes elements_chain, so we reconstruct elements here.
var (
	splitChainRegex      = regexp.MustCompile(`(?:[^\s;"]|"(?:\\.|[^"])*")+`)
	splitClassAttributes = regexp.MustCompile(`(.*?)($|:([a-zA-Z\-\_0-9]*=.*))`)
	parseAttributesRegex = regexp.MustCompile(`(.*?)="(.*?[^\\])"`)
)

// addElementsFromChain reconstructs the parsed elements array from each item's
// elements_chain string, restoring the column the REST endpoint used to emit.
func addElementsFromChain(items []map[string]interface{}) {
	for _, item := range items {
		chain, ok := item["elements_chain"].(string)
		if !ok {
			continue
		}
		item["elements"] = parseElementsChain(chain)
	}
}

func parseElementsChain(chain string) []map[string]interface{} {
	elements := []map[string]interface{}{}
	for idx, elString := range splitChainRegex.FindAllString(chain, -1) {
		m := splitClassAttributes.FindStringSubmatch(elString)
		element := map[string]interface{}{
			"event":       nil,
			"text":        nil,
			"tag_name":    nil,
			"attr_class":  nil,
			"href":        nil,
			"attr_id":     nil,
			"nth_child":   nil,
			"nth_of_type": nil,
			"attributes":  map[string]interface{}{},
			"order":       idx,
		}
		var tagAndClass, attrString string
		if m != nil {
			tagAndClass = m[1]
			if len(m) > 3 {
				attrString = m[3]
			}
		}
		if tagAndClass != "" {
			parts := strings.SplitN(tagAndClass, ".", 2)
			element["tag_name"] = parts[0]
			if len(parts) > 1 {
				classes := []string{}
				for _, cl := range strings.Split(parts[1], ".") {
					if cl != "" {
						classes = append(classes, cl)
					}
				}
				element["attr_class"] = classes
			}
		}
		attributes := element["attributes"].(map[string]interface{})
		for _, am := range parseAttributesRegex.FindAllStringSubmatch(attrString, -1) {
			key, value := am[1], am[2]
			switch key {
			case "href":
				element["href"] = value
			case "nth-child":
				if n, err := strconv.Atoi(value); err == nil {
					element["nth_child"] = n
				}
			case "nth-of-type":
				if n, err := strconv.Atoi(value); err == nil {
					element["nth_of_type"] = n
				}
			case "text":
				element["text"] = value
			case "attr_id":
				element["attr_id"] = value
			default:
				if key != "" {
					attributes[key] = value
				}
			}
		}
		elements = append(elements, element)
	}
	return elements
}
