package resolver

func LoadAndMerge(loader LoaderFunc, rootURI string) (*Catalog, error) {
	graph, err := NewResolver(loader).Resolve(rootURI)
	if err != nil {
		return nil, err
	}
	return Merge(graph)
}
