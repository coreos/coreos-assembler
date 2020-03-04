package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/gedex/inflector"
	"github.com/idubinskiy/schematyper/stringset"
)

//go:generate schematyper --root-type=metaSchema --prefix=meta metaschema.json

var (
	outToStdout     = kingpin.Flag("console", "output to console instead of file").Default("false").Short('c').Bool()
	outputFile      = kingpin.Flag("out-file", "filename for output; default is <schema>_schematype.go").Short('o').String()
	packageName     = kingpin.Flag("package", `package name for generated file; default is "main"`).Default("main").String()
	rootTypeName    = kingpin.Flag("root-type", `name of root type; default is generated from the filename`).String()
	typeNamesPrefix = kingpin.Flag("prefix", `prefix for non-root types`).String()
	ptrForOmit      = kingpin.Flag("ptr-for-omit", "use a pointer to a struct for an object property that is represented as a struct if the property is not required (i.e., has omitempty tag)").Default("false").Bool()
	inputFile       = kingpin.Arg("input", "file containing a valid JSON schema").Required().ExistingFile()
)

type structField struct {
	Name         string
	TypeRef      string
	TypePrefix   string
	Nullable     bool
	PropertyName string
	Required     bool
	Embedded     bool
	PtrForOmit   bool
}

type structFields []structField

func (s structFields) Len() int {
	return len(s)
}

func (s structFields) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

func (s structFields) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type goType struct {
	Name       string
	TypeRef    string
	TypePrefix string
	Nullable   bool
	Fields     structFields
	Comment    string

	parentPath     string
	origTypeName   string
	ambiguityDepth int
}

func (gt goType) print(buf *bytes.Buffer) {
	if gt.Comment != "" {
		commentLines := strings.Split(gt.Comment, "\n")
		for _, line := range commentLines {
			buf.WriteString(fmt.Sprintf("// %s\n", line))
		}
	}
	typeStr := gt.TypePrefix
	baseType, ok := types[gt.TypeRef]
	if ok {
		typeStr += baseType.Name
	}
	buf.WriteString(fmt.Sprintf("type %s %s", gt.Name, typeStr))
	if typeStr != typeStruct {
		buf.WriteString("\n")
		return
	}
	buf.WriteString(" {\n")
	sort.Stable(gt.Fields)
	for _, sf := range gt.Fields {
		sfTypeStr := sf.TypePrefix
		sfBaseType, ok := types[sf.TypeRef]
		if ok {
			sfTypeStr += sfBaseType.Name
		}
		if sf.Nullable && sfTypeStr != typeEmptyInterface {
			sfTypeStr = "*" + sfTypeStr
		}

		var tagString string
		if !sf.Embedded {
			tagString = "`json:\"" + sf.PropertyName
			if !sf.Required {
				if *ptrForOmit && sf.PtrForOmit && !sf.Nullable {
					sfTypeStr = "*" + sfTypeStr
				}
				tagString += ",omitempty"
			}
			tagString += "\"`"
		}
		buf.WriteString(fmt.Sprintf("%s %s %s\n", sf.Name, sfTypeStr, tagString))
	}
	buf.WriteString("}\n")
}

type goTypes []goType

func (t goTypes) Len() int {
	return len(t)
}

func (t goTypes) Less(i, j int) bool {
	return t[i].Name < t[j].Name
}

func (t goTypes) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

var needTimeImport bool

const (
	typeString              = "string"
	typeInteger             = "integer"
	typeInt                 = "int"
	typeNumber              = "number"
	typeFloat64             = "float64"
	typeBoolean             = "boolean"
	typeBool                = "bool"
	typeNull                = "null"
	typeNil                 = "nil"
	typeObject              = "object"
	typeArray               = "array"
	typeEmptyInterface      = "interface{}"
	typeEmptyInterfaceSlice = "[]interface{}"
	typeTime                = "time.Time"
	typeStruct              = "struct"
)

var typeStrings = map[string]string{
	typeString:  typeString,
	typeInteger: typeInt,
	typeNumber:  typeFloat64,
	typeBoolean: typeBool,
	typeNull:    typeNil,
	typeObject:  typeObject,
	typeArray:   typeArray,
}

func getTypeString(jsonType, format string) string {
	if format == "date-time" {
		needTimeImport = true
		return typeTime
	}

	if ts, ok := typeStrings[jsonType]; ok {
		return ts
	}
	return typeEmptyInterface
}

// copied from golint (https://github.com/golang/lint/blob/4946cea8b6efd778dc31dc2dbeb919535e1b7529/lint.go#L701)
var commonInitialisms = stringset.New(
	"API",
	"ASCII",
	"CPU",
	"CSS",
	"DNS",
	"EOF",
	"GUID",
	"HTML",
	"HTTP",
	"HTTPS",
	"ID",
	"IP",
	"JSON",
	"LHS",
	"QPS",
	"RAM",
	"RHS",
	"RPC",
	"SLA",
	"SMTP",
	"SQL",
	"SSH",
	"TCP",
	"TLS",
	"TTL",
	"UDP",
	"UI",
	"UID",
	"UUID",
	"URI",
	"URL",
	"UTF8",
	"VM",
	"XML",
	"XSRF",
	"XSS",
)

func dashedToWords(s string) string {
	return regexp.MustCompile("-|_").ReplaceAllString(s, " ")
}

func camelCaseToWords(s string) string {
	return regexp.MustCompile(`([\p{Ll}\p{N}])(\p{Lu})`).ReplaceAllString(s, "$1 $2")
}

func getExportedIdentifierPart(part string) string {
	upperedPart := strings.ToUpper(part)
	if commonInitialisms.Has(upperedPart) {
		return upperedPart
	}
	return strings.Title(strings.ToLower(part))
}

func generateIdentifier(origName string, exported bool) string {
	spacedName := camelCaseToWords(dashedToWords(origName))
	titledName := strings.Title(spacedName)
	nameParts := strings.Split(titledName, " ")
	for i, part := range nameParts {
		nameParts[i] = getExportedIdentifierPart(part)
	}
	if !exported {
		nameParts[0] = strings.ToLower(nameParts[0])
	}
	rawName := strings.Join(nameParts, "")

	// make sure we build a valid identifier
	buf := &bytes.Buffer{}
	for pos, char := range rawName {
		if unicode.IsLetter(char) || char == '_' || (unicode.IsDigit(char) && pos > 0) {
			buf.WriteRune(char)
		}
	}

	return buf.String()
}

func generateTypeName(origName string) string {
	if *packageName != "main" || *typeNamesPrefix != "" {
		return *typeNamesPrefix + generateIdentifier(origName, true)
	}

	return generateIdentifier(origName, false)
}

func generateFieldName(origName string) string {
	return generateIdentifier(origName, true)
}

func getTypeSchema(typeInterface interface{}) *metaSchema {
	typeSchemaJSON, _ := json.Marshal(typeInterface)
	var typeSchema metaSchema
	json.Unmarshal(typeSchemaJSON, &typeSchema)
	return &typeSchema
}

func getTypeSchemas(typeInterface interface{}) map[string]*metaSchema {
	typeSchemasJSON, _ := json.Marshal(typeInterface)
	var typeSchemas map[string]*metaSchema
	json.Unmarshal(typeSchemasJSON, &typeSchemas)
	return typeSchemas
}

func singularize(plural string) string {
	singular := inflector.Singularize(plural)
	if singular == plural {
		singular += "Item"
	}
	return singular
}

func parseAdditionalProperties(ap interface{}) (hasAddl bool, addlSchema *metaSchema) {
	switch ap := ap.(type) {
	case bool:
		return ap, nil
	case map[string]interface{}:
		return true, getTypeSchema(ap)
	default:
		return
	}
}

type deferredType struct {
	schema     *metaSchema
	name       string
	desc       string
	parentPath string
}

type stringSetMap map[string]stringset.StringSet

func (m stringSetMap) addTo(set, val string) {
	if m[set] == nil {
		m[set] = stringset.New()
	}
	m[set].Add(val)
}

func (m stringSetMap) removeFrom(set, val string) {
	if m[set] == nil {
		return
	}
	m[set].Remove(val)
}

func (m stringSetMap) existsIn(set, val string) bool {
	if m[set] == nil {
		return false
	}
	return m[set].Has(val)
}

func (m stringSetMap) delete(set string) {
	delete(m, set)
}

func (m stringSetMap) has(set string) bool {
	_, ok := m[set]
	return ok
}

var types = make(map[string]goType)
var deferredTypes = make(map[string]deferredType)
var typesByName = make(stringSetMap)
var transitiveRefs = make(map[string]string)

func processType(s *metaSchema, pName, pDesc, path, parentPath string) (typeRef string) {
	if len(s.Definitions) > 0 {
		parseDefs(s, path)
	}

	var gt goType

	// avoid 'recursive type' problem, at least for the root type
	if path == "#" {
		gt.Nullable = true
	}

	if s.Ref != "" {
		ref, ok := transitiveRefs[s.Ref]
		if !ok {
			ref = s.Ref
		}
		if _, ok := types[ref]; ok {
			transitiveRefs[path] = ref
			return ref
		}
		deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
		return ""
	}

	gt.parentPath = parentPath

	if path == "#" {
		gt.origTypeName = *rootTypeName
		gt.Name = *rootTypeName
	} else {
		gt.origTypeName = s.Title
		if gt.origTypeName == "" {
			gt.origTypeName = pName
		}

		if gt.Name = generateTypeName(gt.origTypeName); gt.Name == "" {
			log.Fatalln("Can't generate type without name.")
		}
	}

	typeRef = path

	gt.Comment = s.Description
	if gt.Comment == "" {
		gt.Comment = pDesc
	}

	required := stringset.New()
	for _, req := range s.Required {
		required.Add(string(req))
	}

	defer func() {
		types[path] = gt
		typesByName.addTo(gt.Name, path)
	}()

	var jsonType string
	switch schemaType := s.Type.(type) {
	case []interface{}:
		if len(schemaType) == 2 && (schemaType[0] == typeNull || schemaType[1] == typeNull) {
			gt.Nullable = true

			jsonType = schemaType[0].(string)
			if jsonType == typeNull {
				jsonType = schemaType[1].(string)
			}
		}
	case string:
		jsonType = schemaType
	}

	hasAllOf := len(s.AllOf) > 0
	if jsonType == "" && hasAllOf {
		for index, allOfSchema := range s.AllOf {
			childPath := fmt.Sprintf("%s/allOf/%d", path, index)
			gotType := processType(&allOfSchema, fmt.Sprintf("%sEmbedded%d", pName, index), allOfSchema.Description, childPath, path)
			if gotType == "" {
				deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
				return ""
			}
			childType := types[gotType]
			// if any chid is an object, the parent is an object
			if childType.TypePrefix == "struct" {
				jsonType = "object"
			}
			// if any child is nullable, the parent is nullable
			if childType.Nullable {
				gt.Nullable = true
			}
		}
	}

	props := getTypeSchemas(s.Properties)
	hasProps := len(props) > 0
	hasAddlProps, addlPropsSchema := parseAdditionalProperties(s.AdditionalProperties)

	ts := getTypeString(jsonType, s.Format)
	switch ts {
	case typeObject:
		if gt.Name == "Properties" {
			panic(fmt.Errorf("props: %+v\naddlPropsSchema: %+v\n", props, addlPropsSchema))
		}
		if (hasProps || hasAllOf) && !hasAddlProps {
			gt.TypePrefix = typeStruct
		} else if !hasProps && !hasAllOf && hasAddlProps && addlPropsSchema != nil {
			singularName := singularize(gt.origTypeName)
			gotType := processType(addlPropsSchema, singularName, s.Description, path+"/additionalProperties", path)
			if gotType == "" {
				deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
				return ""
			}
			gt.TypePrefix = "map[string]"
			gt.TypeRef = gotType
		} else {
			gt.TypePrefix = "map[string]interface{}"
		}
	case typeArray:
		switch arrayItemType := s.Items.(type) {
		case []interface{}:
			if len(arrayItemType) == 1 {
				singularName := singularize(gt.origTypeName)
				typeSchema := getTypeSchema(arrayItemType[0])
				gotType := processType(typeSchema, singularName, s.Description, path+"/items/0", path)
				if gotType == "" {
					deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
					return ""
				}
				gt.TypePrefix = "[]"
				gt.TypeRef = gotType
			} else {
				gt.TypePrefix = typeEmptyInterfaceSlice
			}
		case interface{}:
			singularName := singularize(gt.origTypeName)
			typeSchema := getTypeSchema(arrayItemType)
			gotType := processType(typeSchema, singularName, s.Description, path+"/items", path)
			if gotType == "" {
				deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
				return ""
			}
			gt.TypePrefix = "[]"
			gt.TypeRef = gotType
		default:
			gt.TypePrefix = typeEmptyInterfaceSlice
		}
	default:
		gt.TypePrefix = ts
	}

	for propName, propSchema := range props {
		sf := structField{
			PropertyName: propName,
			Required:     required.Has(propName),
		}

		var fieldName string
		if propSchema.Title != "" {
			fieldName = propSchema.Title
		} else {
			fieldName = propName
		}
		if sf.Name = generateFieldName(fieldName); sf.Name == "" {
			log.Fatalln("Can't generate field without name.")
		}

		if propSchema.Ref != "" {
			if refType, ok := types[propSchema.Ref]; ok {
				sf.TypeRef, sf.Nullable = propSchema.Ref, refType.Nullable
				if refType.TypePrefix == typeStruct {
					sf.PtrForOmit = true
				}
				gt.Fields = append(gt.Fields, sf)
				continue
			}
			deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
			return ""
		}

		switch propType := propSchema.Type.(type) {
		case []interface{}:
			if len(propType) == 2 && (propType[0] == typeNull || propType[1] == typeNull) {
				sf.Nullable = true

				jsonType := propType[0]
				if jsonType == typeNull {
					jsonType = propType[1]
				}

				sf.TypePrefix = getTypeString(jsonType.(string), propSchema.Format)
			}
		case string:
			sf.TypePrefix = getTypeString(propType, propSchema.Format)
		case nil:
			sf.TypePrefix = typeEmptyInterface
		}

		refPath := path + "/properties/" + propName

		props := getTypeSchemas(propSchema.Properties)
		hasProps := len(props) > 0
		hasAddlProps, addlPropsSchema := parseAdditionalProperties(propSchema.AdditionalProperties)

		if sf.TypePrefix == typeObject {
			if hasProps && !hasAddlProps {
				gotType := processType(propSchema, sf.Name, propSchema.Description, refPath, path)
				if gotType == "" {
					deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
					return ""
				}
				sf.TypePrefix = ""
				sf.TypeRef = gotType
				sf.PtrForOmit = true
			} else if !hasProps && hasAddlProps && addlPropsSchema != nil {
				singularName := singularize(propName)
				gotType := processType(addlPropsSchema, singularName, propSchema.Description, refPath+"/additionalProperties", path)
				if gotType == "" {
					deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
					return ""
				}
				sf.TypePrefix = "map[string]"
				sf.TypeRef = gotType
			} else {
				sf.TypePrefix = "map[string]interface{}"
			}
		} else if sf.TypePrefix == typeArray {
			switch arrayItemType := propSchema.Items.(type) {
			case []interface{}:
				if len(arrayItemType) == 1 {
					singularName := singularize(propName)
					typeSchema := getTypeSchema(arrayItemType[0])
					gotType := processType(typeSchema, singularName, propSchema.Description, refPath+"/items/0", path)
					if gotType == "" {
						deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
						return ""
					}
					sf.TypePrefix = "[]"
					sf.TypeRef = gotType
				} else {
					sf.TypePrefix = typeEmptyInterfaceSlice
				}
			case interface{}:
				singularName := singularize(propName)
				typeSchema := getTypeSchema(arrayItemType)
				gotType := processType(typeSchema, singularName, propSchema.Description, refPath+"/items", path)
				if gotType == "" {
					deferredTypes[path] = deferredType{schema: s, name: pName, desc: pDesc, parentPath: parentPath}
					return ""
				}
				sf.TypePrefix = "[]"
				sf.TypeRef = gotType
			default:
				sf.TypePrefix = typeEmptyInterfaceSlice
			}
		}

		gt.Fields = append(gt.Fields, sf)
	}

	for index := range s.AllOf {
		sf := structField{
			Embedded: true,
		}

		childPath := fmt.Sprintf("%s/allOf/%d", path, index)
		if _, ok := transitiveRefs[childPath]; ok {
			childPath = transitiveRefs[childPath]
		}
		sf.TypeRef = childPath

		gt.Fields = append(gt.Fields, sf)
	}

	return
}

func processDeferred() {
	for len(deferredTypes) > 0 {
		startDeferredPaths, _ := stringset.FromMapKeys(deferredTypes)
		for _, path := range startDeferredPaths.Sorted() {
			deferred := deferredTypes[path]
			name := processType(deferred.schema, deferred.name, deferred.desc, path, deferred.parentPath)
			if name != "" {
				delete(deferredTypes, path)
			}
		}

		// if the list is the same as before, we're stuck
		endDeferredPaths, _ := stringset.FromMapKeys(deferredTypes)
		if endDeferredPaths.Equals(startDeferredPaths) {
			log.Fatalln("Can't resolve:", startDeferredPaths)
		}
	}
}

func dedupeTypes() {
	for len(typesByName) > 0 {
		// clear all singles first; otherwise some types will not be disambiguated
		for name, dupes := range typesByName {
			if len(dupes) == 1 {
				typesByName.delete(name)
			}
		}

		newTypesByName := make(stringSetMap)

		typeNames, _ := stringset.FromMapKeys(typesByName)
		sortedTypeNames := typeNames.Sorted()

		for _, name := range sortedTypeNames {
			dupes := typesByName[name]
			// delete these dupes; will put back in as necessary in subsequent loop
			typesByName.delete(name)

		dupesLoop:
			for _, dupePath := range dupes.Sorted() {
				gt := types[dupePath]
				gt.ambiguityDepth++

				topChild := gt
				var parent goType
				for i := 0; i < gt.ambiguityDepth; i++ {
					parent = types[topChild.parentPath]

					// handle parents before children to avoid stuttering
					if typesByName.has(parent.Name) {
						// add back the child to be processed later
						newTypesByName.addTo(gt.Name, dupePath)
						gt.ambiguityDepth--
						continue dupesLoop
					}

					topChild = parent
				}

				if parent.origTypeName == "" {
					log.Fatalln("Can't disabiguate:", dupes)
				}

				gt.origTypeName = parent.origTypeName + "-" + gt.origTypeName

				gt.Name = generateTypeName(gt.origTypeName)
				types[dupePath] = gt

				// add with new name in case we still have dupes
				newTypesByName.addTo(gt.Name, dupePath)
			}
		}
		typesByName = newTypesByName
	}
}

func parseDefs(s *metaSchema, path string) {
	defs := getTypeSchemas(s.Definitions)
	for defName, defSchema := range defs {
		name := processType(defSchema, defName, defSchema.Description, path+"/definitions/"+defName, path)
		if name == "" {
			deferredTypes[path+"/definitions/"+defName] = deferredType{schema: defSchema, name: defName, desc: defSchema.Description, parentPath: path}
		}
	}
}

func main() {
	kingpin.Parse()

	file, err := ioutil.ReadFile(*inputFile)
	if err != nil {
		log.Fatalln("Error reading file:", err)
	}

	var s metaSchema
	if err = json.Unmarshal(file, &s); err != nil {
		log.Fatalln("Error parsing JSON:", err)
	}

	schemaName := strings.Split(filepath.Base(*inputFile), ".")[0]
	if *rootTypeName == "" {
		exported := *packageName != "main"
		*rootTypeName = generateIdentifier(schemaName, exported)
	}
	processType(&s, *rootTypeName, s.Description, "#", "")
	processDeferred()
	dedupeTypes()

	var resultSrc bytes.Buffer
	resultSrc.WriteString(fmt.Sprintln("package", *packageName))
	resultSrc.WriteString(fmt.Sprintf("\n// generated by \"%s\" -- DO NOT EDIT\n", strings.Join(os.Args, " ")))
	resultSrc.WriteString("\n")
	if needTimeImport {
		resultSrc.WriteString("import \"time\"\n")
	}
	typesSlice := make(goTypes, 0, len(types))
	for _, gt := range types {
		typesSlice = append(typesSlice, gt)
	}
	sort.Stable(typesSlice)
	for _, gt := range typesSlice {
		gt.print(&resultSrc)
		resultSrc.WriteString("\n")
	}
	formattedSrc, err := format.Source(resultSrc.Bytes())
	if err != nil {
		fmt.Println(resultSrc.String())
		log.Fatalln("Error running gofmt:", err)
	}

	if *outToStdout {
		fmt.Print(string(formattedSrc))
	} else {
		outputFileName := *outputFile
		if outputFileName == "" {
			compactSchemaName := strings.ToLower(*rootTypeName)
			outputFileName = fmt.Sprintf("%s_schematype.go", compactSchemaName)
		}
		err = ioutil.WriteFile(outputFileName, formattedSrc, 0644)
		if err != nil {
			log.Fatalf("Error writing to %s: %s\n", outputFileName, err)
		}
	}
}
