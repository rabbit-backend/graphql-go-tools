package tools

import (
	"context"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// default root type names
const (
	DefaultRootQueryName        = "Query"
	DefaultRootMutationName     = "Mutation"
	DefaultRootSubscriptionName = "Subscription"
)

// MakeExecutableSchema is shorthand for ExecutableSchema{}.Make(ctx context.Context)
func MakeExecutableSchema(config ExecutableSchema) (graphql.Schema, error) {
	return config.Make(context.Background())
}

// MakeExecutableSchemaWithContext make a schema and supply a context
func MakeExecutableSchemaWithContext(ctx context.Context, config ExecutableSchema) (graphql.Schema, error) {
	return config.Make(ctx)
}

// ExecutableSchema configuration for making an executable schema
// this attempts to provide similar functionality to Apollo graphql-tools
// https://www.apollographql.com/docs/graphql-tools/generate-schema
type ExecutableSchema struct {
	document         *ast.Document
	TypeDefs         interface{}               // a string, []string, or func() []string
	Resolvers        map[string]interface{}    // a map of Resolver, Directive, Scalar, Enum, Object, InputObject, Union, or Interface
	SchemaDirectives SchemaDirectiveVisitorMap // Map of SchemaDirectiveVisitor
	Extensions       []graphql.Extension       // GraphQL extensions
	Debug            bool                      // Prints debug messages during compile
}

// Document returns the document
func (c *ExecutableSchema) Document() *ast.Document {
	return c.document
}

// Make creates a graphql schema config, this struct maintains intact the types and does not require the use of a non empty Query
func (c *ExecutableSchema) Make(ctx context.Context) (graphql.Schema, error) {
	// combine the TypeDefs
	document, err := c.ConcatenateTypeDefs()
	if err != nil {
		return graphql.Schema{}, err
	}

	c.document = document

	// create a new registry
	registry, err := newRegistry(ctx, c.Resolvers, c.SchemaDirectives, c.Extensions, document)
	if err != nil {
		return graphql.Schema{}, err
	}

	if registry.dependencyMap, err = registry.IdentifyDependencies(); err != nil {
		return graphql.Schema{}, err
	}

	// resolve the document definitions
	if err := registry.resolveDefinitions(); err != nil {
		return graphql.Schema{}, err
	}

	// check if schema was created by definition
	if registry.schema != nil {
		return *registry.schema, nil
	}

	// otherwise build a schema from default object names
	query, err := registry.getObject(DefaultRootQueryName)
	if err != nil {
		return graphql.Schema{}, err
	}

	mutation, _ := registry.getObject(DefaultRootMutationName)
	subscription, _ := registry.getObject(DefaultRootSubscriptionName)

	// create a new schema config
	schemaConfig := &graphql.SchemaConfig{
		Query:        query,
		Mutation:     mutation,
		Subscription: subscription,
		Types:        registry.typeArray(),
		Directives:   registry.directiveArray(),
		Extensions:   c.Extensions,
	}

	schema, err := graphql.NewSchema(*schemaConfig)
	// create a new schema
	return schema, err
}

// build a schema from an ast
func (c *registry) buildSchemaFromAST(definition *ast.SchemaDefinition) error {
	schemaConfig := &graphql.SchemaConfig{
		Types:      c.typeArray(),
		Directives: c.directiveArray(),
		Extensions: c.extensions,
	}

	// add operations
	for _, op := range definition.OperationTypes {
		switch op.Operation {
		case ast.OperationTypeQuery:
			if object, err := c.getObject(op.Type.Name.Value); err == nil {
				schemaConfig.Query = object
			} else {
				return err
			}
		case ast.OperationTypeMutation:
			if object, err := c.getObject(op.Type.Name.Value); err == nil {
				schemaConfig.Mutation = object
			} else {
				return err
			}
		case ast.OperationTypeSubscription:
			if object, err := c.getObject(op.Type.Name.Value); err == nil {
				schemaConfig.Subscription = object
			} else {
				return err
			}
		}
	}

	// apply schema directives
	if err := c.applyDirectives(applyDirectiveParams{
		config:     schemaConfig,
		directives: definition.Directives,
		node:       definition,
	}); err != nil {
		return err
	}

	// build the schema
	schema, err := graphql.NewSchema(*schemaConfig)
	if err != nil {
		return err
	}

	c.schema = &schema
	return nil
}
