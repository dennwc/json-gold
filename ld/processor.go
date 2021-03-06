package ld

import (
	"strings"
)

// JsonLdProcessor implements the JsonLdProcessor interface, see
// http://www.w3.org/TR/json-ld-api/#the-jsonldprocessor-interface
//
type JsonLdProcessor struct {
}

// NewJsonLdProcessor creates an instance of JsonLdProcessor.
func NewJsonLdProcessor() *JsonLdProcessor {
	return &JsonLdProcessor{}
}

// Compact operation compacts the given input using the context according to the steps
// in the Compaction algorithm: http://www.w3.org/TR/json-ld-api/#compaction-algorithm
func (jldp *JsonLdProcessor) Compact(input interface{}, context interface{},
	opts *JsonLdOptions) (map[string]interface{}, error) {
	// 1)
	// TODO: look into promises

	// 2-6) NOTE: these are all the same steps as in expand
	expanded, err := jldp.expand(input, opts)
	if err != nil {
		return nil, err
	}

	// 7)
	contextMap, isMap := context.(map[string]interface{})
	innerCtx, hasCtx := contextMap["@context"]
	if isMap && hasCtx {
		context = innerCtx
	}
	activeCtx := NewContext(nil, opts)
	activeCtx, err = activeCtx.Parse(context)
	if err != nil {
		return nil, err
	}

	// 8)
	api := NewJsonLdApi()
	compacted, err := api.Compact(activeCtx, "", expanded, opts.CompactArrays)
	if err != nil {
		return nil, err
	}

	// final step of Compaction Algorithm
	// TODO: SPEC: the result result is a NON EMPTY array,
	if compactedList, isList := compacted.([]interface{}); isList {
		if len(compactedList) == 0 {
			compacted = make(map[string]interface{})
		} else {
			// TODO: SPEC: doesn't specify to use vocab = true here
			compactedIRI := activeCtx.CompactIri("@graph", nil, true, false)
			compacted = map[string]interface{}{
				compactedIRI: compacted,
			}
		}
	}

	contextMap, _ = context.(map[string]interface{})
	contextList, _ := context.([]interface{})
	contextIsNotEmpty := len(contextMap) > 0 || len(contextList) > 0
	if compactedMap, isMap := compacted.(map[string]interface{}); contextIsNotEmpty && isMap {
		// TODO: figure out if we can make "@context" appear at the start of the keySet
		compactedMap["@context"] = context
	}

	// 9)
	return compacted.(map[string]interface{}), nil
}

// Expand operation expands the given input according to the steps in the Expansion algorithm:
// http://www.w3.org/TR/json-ld-api/#expansion-algorithm
func (jldp *JsonLdProcessor) Expand(input interface{}, opts *JsonLdOptions) ([]interface{}, error) {
	return jldp.expand(input, opts)
}

func (jldp *JsonLdProcessor) expand(input interface{}, opts *JsonLdOptions) ([]interface{}, error) {
	// 1)
	// TODO: look into promises

	var remoteContext string

	// 2)
	if iri, isString := input.(string); isString && strings.Contains(iri, ":") {
		rd, err := opts.DocumentLoader.LoadDocument(iri)
		if err != nil {
			return nil, err
		}
		if rd.Document == "" {
			return nil, NewJsonLdError(LoadingDocumentFailed, err)
		}
		input = rd.Document
		iri = rd.DocumentURL

		// if set the base in options should override the base iri in the
		// active context
		// thus only set this as the base iri if it's not already set in
		// options
		if opts.Base == "" {
			opts.Base = iri
		}

		if rd.ContextURL != "" {
			remoteContext = rd.ContextURL
		}
	}
	// 3)
	activeCtx := NewContext(nil, opts)

	// 4)
	if opts.ExpandContext != nil {
		exCtx := opts.ExpandContext
		if exCtxMap, isMap := exCtx.(map[string]interface{}); isMap {
			if ctx, hasCtx := exCtxMap["@context"]; hasCtx {
				exCtx = ctx
			}
		}

		var err error
		activeCtx, err = activeCtx.Parse(exCtx)
		if err != nil {
			return nil, err
		}
	}

	// 5)
	if remoteContext != "" {
		var err error
		if activeCtx, err = activeCtx.Parse(remoteContext); err != nil {
			return nil, err
		}
	}

	// 6)
	api := NewJsonLdApi()
	expanded, err := api.Expand(activeCtx, "", input)
	if err != nil {
		return nil, err
	}

	// final step of Expansion Algorithm
	expandedMap, isMap := expanded.(map[string]interface{})

	if isMap && len(expandedMap) == 0 {
		expanded = nil
	}

	graph, hasGraph := expandedMap["@graph"]
	if isMap && hasGraph && len(expandedMap) == 1 {
		expanded = graph
	} else if expanded == nil {
		expanded = make([]interface{}, 0)
	}

	// normalize to an array
	if expandedList, isList := expanded.([]interface{}); isList {
		return expandedList, nil
	}

	return []interface{}{expanded}, nil
}

// Flatten operation flattens the given input and compacts it using the passed context
// according to the steps in the Flattening algorithm:
// http://www.w3.org/TR/json-ld-api/#flattening-algorithm
func (jldp *JsonLdProcessor) Flatten(input interface{}, context interface{}, opts *JsonLdOptions) (interface{}, error) {
	idGen := NewBlankNodeIDGenerator()

	// 2-6) NOTE: these are all the same steps as in expand
	expanded, err := jldp.expand(input, opts)
	if err != nil {
		return nil, err
	}
	// 7)
	contextMap, isMap := context.(map[string]interface{})
	innerCtx, hasCtx := contextMap["@context"]
	if isMap && hasCtx {
		context = innerCtx
	}

	// 9) NOTE: the next block is the Flattening Algorithm described in
	// http://json-ld.org/spec/latest/json-ld-api/#flattening-algorithm

	// 1)
	nodeMap := make(map[string]interface{})
	nodeMap["@default"] = make(map[string]interface{})
	// 2)
	api := NewJsonLdApi()
	if err = api.GenerateNodeMap(expanded, nodeMap, "@default", nil, "", nil, idGen); err != nil {
		return nil, err
	}

	// 3)
	defaultGraph := nodeMap["@default"].(map[string]interface{})
	delete(nodeMap, "@default")

	// 4)
	for _, graphName := range GetKeys(nodeMap) {
		graph := nodeMap[graphName].(map[string]interface{})
		// 4.1+4.2)
		var entry map[string]interface{}
		if _, present := defaultGraph[graphName]; !present {
			entry = make(map[string]interface{})
			entry["@id"] = graphName
			defaultGraph[graphName] = entry
		} else {
			entry = defaultGraph[graphName].(map[string]interface{})
		}
		// 4.3)
		// TODO: SPEC doesn't specify that this should only be added if it
		// doesn't exists
		if _, present := entry["@graph"]; !present {
			entry["@graph"] = make([]interface{}, 0)
		}

		for _, id := range GetOrderedKeys(graph) {
			node := graph[id].(map[string]interface{})
			if _, present := node["@id"]; !(present && len(node) == 1) {
				entry["@graph"] = append(entry["@graph"].([]interface{}), node)
			}
		}
	}

	// 5)
	flattened := make([]interface{}, 0)

	// 6)
	for _, id := range GetOrderedKeys(defaultGraph) {
		node := defaultGraph[id].(map[string]interface{})
		if _, present := node["@id"]; !(present && len(node) == 1) {
			flattened = append(flattened, node)
		}
	}
	// 8)
	if context != nil && len(flattened) > 0 {
		activeCtx := NewContext(nil, opts)
		activeCtx, err = activeCtx.Parse(context)
		if err != nil {
			return nil, err
		}

		compacted, err := api.Compact(activeCtx, "", flattened, opts.CompactArrays)
		if err != nil {
			return nil, err
		}

		if _, isList := compacted.([]interface{}); !isList {
			compacted = []interface{}{compacted}
		}
		alias := activeCtx.CompactIri("@graph", nil, false, false)
		rval := activeCtx.Serialize()
		rval[alias] = compacted
		return rval, nil
	}
	return flattened, nil
}

// Frame operation frames the given input using the frame according to the steps in the Framing Algorithm:
// http://json-ld.org/spec/latest/json-ld-framing/#framing-algorithm
//
// input: The input JSON-LD object
// frame: The frame to use when re-arranging the data of input; either in the form of an JSON object or as IRI.
//
// Returns the framed JSON-LD document.
func (jldp *JsonLdProcessor) Frame(input interface{}, frame interface{}, opts *JsonLdOptions) (map[string]interface{}, error) {

	if _, isMap := frame.(map[string]interface{}); isMap {
		frame = CloneDocument(frame)
	}

	expandedInput, err := jldp.Expand(input, opts)
	if err != nil {
		return nil, err
	}
	expandedFrame, err := jldp.Expand(frame, opts)
	if err != nil {
		return nil, err
	}

	api := NewJsonLdApi()

	framed, err := api.Frame(expandedInput, expandedFrame, opts)
	if err != nil {
		return nil, err
	}

	frameMap := frame.(map[string]interface{})
	activeCtx := NewContext(nil, opts)
	activeCtx, err = activeCtx.Parse(frameMap["@context"])
	if err != nil {
		return nil, err
	}

	compacted, _ := api.Compact(activeCtx, "", framed, true)
	if _, isList := compacted.([]interface{}); !isList {
		compacted = []interface{}{compacted}
	}
	alias := activeCtx.CompactIri("@graph", nil, false, false)
	rval := activeCtx.Serialize()
	rval[alias] = compacted
	RemovePreserve(activeCtx, rval, opts)
	return rval, nil
}

var rdfSerializers = map[string]RDFSerializer{
	"application/nquads": &NQuadRDFSerializer{},
	"text/turtle":        &TurtleRDFSerializer{},
}

// FromRDF converts an RDF dataset to JSON-LD.
//
// dataset: a serialized string of RDF in a format specified by the format option or an RDF dataset to convert.
// opts: the options to use:
//     [format] the format if input is not an array: 'application/nquads' for N-Quads (default).
//     [useRdfType] true to use rdf:type, false to use @type (default: false).
//     [useNativeTypes] true to convert XSD types into native types (boolean, integer, double),
//     false not to (default: true).
func (jldp *JsonLdProcessor) FromRDF(dataset interface{}, opts *JsonLdOptions) (interface{}, error) {

	// handle non specified serializer case
	if _, isString := dataset.(string); opts.Format == "" && isString {
		// attempt to parse the input as nquads
		opts.Format = "application/nquads"
	}

	serializer, hasSerializer := rdfSerializers[opts.Format]
	if !hasSerializer {
		return nil, NewJsonLdError(UnknownFormat, opts.Format)
	}

	// convert from RDF
	return jldp.fromRDF(dataset, opts, serializer)
}

func (jldp *JsonLdProcessor) fromRDF(input interface{}, opts *JsonLdOptions, serializer RDFSerializer) (interface{}, error) {

	dataset, _ := serializer.Parse(input)

	// convert from RDF
	api := NewJsonLdApi()
	rval, err := api.FromRDF(dataset, opts)
	if err != nil {
		return nil, err
	}

	// re-process using the generated context if outputForm is set
	if opts.OutputForm != "" {
		if opts.OutputForm == "expanded" {
			return rval, nil
		} else if opts.OutputForm == "compacted" {
			return jldp.Compact(rval, dataset.context, opts)
		} else if opts.OutputForm == "flattened" {
			return jldp.Flatten(rval, dataset.context, opts)
		} else {
			return nil, NewJsonLdError(UnknownError, "")
		}
	}
	return rval, nil
}

// ToRDF outputs the RDF dataset found in the given JSON-LD object.
//
// input: the JSON-LD input.
// opts: the options to use:
//     [base] the base IRI to use.
//     [format] the format to use to output a string: 'application/nquads' for N-Quads (default).
//
func (jldp *JsonLdProcessor) ToRDF(input interface{}, opts *JsonLdOptions) (interface{}, error) {

	expandedInput, _ := jldp.expand(input, opts)
	api := NewJsonLdApi()
	dataset, err := api.ToRDF(expandedInput, opts)
	if err != nil {
		return nil, err
	}

	// generate namespaces from context
	if opts.UseNamespaces {
		var _input []map[string]interface{}
		if inputList, isList := input.([]map[string]interface{}); isList {
			_input = inputList
		} else {
			_input = make([]map[string]interface{}, 1)
			_input[0] = make(map[string]interface{})
		}
		for _, e := range _input {
			if ctxVal, hasCtx := e["@context"]; hasCtx {
				dataset.ParseContext(ctxVal, opts)
			}
		}
	}

	if opts.Format != "" {
		serializer, hasSerializer := rdfSerializers[opts.Format]
		if !hasSerializer {
			return nil, NewJsonLdError(UnknownFormat, opts.Format)
		}
		return serializer.Serialize(dataset)
	}

	return dataset, nil
}

// Normalize performs RDF dataset normalization on the given JSON-LD input.
// The output is an RDF dataset unless the 'format' option is used.
func (jldp *JsonLdProcessor) Normalize(input interface{}, opts *JsonLdOptions) (interface{}, error) {

	toRDFOpts := NewJsonLdOptions(opts.Base)
	toRDFOpts.Format = ""
	// it's important to pass the original DocumentLoader. The default one will be used otherwise!
	toRDFOpts.DocumentLoader = opts.DocumentLoader

	datasetObj, err := jldp.ToRDF(input, toRDFOpts)
	if err != nil {
		return nil, err
	}
	dataset := datasetObj.(*RDFDataset)

	api := NewJsonLdApi()
	return api.Normalize(dataset, opts)
}
