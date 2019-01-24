// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package consoleql_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/pkg/auth"
	"storj.io/storj/satellite"
	"storj.io/storj/satellite/console"
	"storj.io/storj/satellite/console/consoleauth"
	"storj.io/storj/satellite/console/consoleweb/consoleql"
	"storj.io/storj/satellite/satellitedb/satellitedbtest"
)

func TestGraphqlQuery(t *testing.T) {
	satellitedbtest.Run(t, func(t *testing.T, db satellite.DB) {
		ctx := testcontext.New(t)
		defer ctx.Cleanup()

		log := zap.NewExample()

		service, err := console.NewService(
			log,
			&consoleauth.Hmac{Secret: []byte("my-suppa-secret-key")},
			db.Console(),
		)

		if err != nil {
			t.Fatal(err)
		}

		creator := consoleql.TypeCreator{}
		if err = creator.Create(service); err != nil {
			t.Fatal(err)
		}

		schema, err := graphql.NewSchema(graphql.SchemaConfig{
			Query:    creator.RootQuery(),
			Mutation: creator.RootMutation(),
		})

		if err != nil {
			t.Fatal(err)
		}

		createUser := console.CreateUser{
			UserInfo: console.UserInfo{
				FirstName: "John",
				LastName:  "",
				Email:     "mtest@email.com",
			},
			Password: "123a123",
		}

		rootUser, err := service.CreateUser(ctx, createUser)
		if err != nil {
			t.Fatal(err)
		}

		token, err := service.Token(ctx, createUser.Email, createUser.Password)
		if err != nil {
			t.Fatal(err)
		}

		sauth, err := service.Authorize(auth.WithAPIKey(ctx, []byte(token)))
		if err != nil {
			t.Fatal(err)
		}

		authCtx := console.WithAuth(ctx, sauth)

		testQuery := func(t *testing.T, query string) interface{} {
			result := graphql.Do(graphql.Params{
				Schema:        schema,
				Context:       authCtx,
				RequestString: query,
				RootObject:    make(map[string]interface{}),
			})

			for _, err := range result.Errors {
				assert.NoError(t, err)
			}

			if result.HasErrors() {
				t.Fatal()
			}

			return result.Data
		}

		t.Run("User query", func(t *testing.T) {
			testUser := func(t *testing.T, actual map[string]interface{}, expected *console.User) {
				assert.Equal(t, expected.ID.String(), actual[consoleql.FieldID])
				assert.Equal(t, expected.Email, actual[consoleql.FieldEmail])
				assert.Equal(t, expected.FirstName, actual[consoleql.FieldFirstName])
				assert.Equal(t, expected.LastName, actual[consoleql.FieldLastName])

				createdAt := time.Time{}
				err := createdAt.UnmarshalText([]byte(actual[consoleql.FieldCreatedAt].(string)))

				assert.NoError(t, err)
				assert.Equal(t, expected.CreatedAt, createdAt)
			}

			t.Run("With ID", func(t *testing.T) {
				query := fmt.Sprintf(
					"query {user(id:\"%s\"){id,email,firstName,lastName,createdAt}}",
					rootUser.ID.String(),
				)

				result := testQuery(t, query)

				data := result.(map[string]interface{})
				user := data[consoleql.UserQuery].(map[string]interface{})

				testUser(t, user, rootUser)
			})

			t.Run("With AuthFallback", func(t *testing.T) {
				query := "query {user{id,email,firstName,lastName,createdAt}}"

				result := testQuery(t, query)

				data := result.(map[string]interface{})
				user := data[consoleql.UserQuery].(map[string]interface{})

				testUser(t, user, rootUser)
			})
		})

		createdProject, err := service.CreateProject(authCtx, console.ProjectInfo{
			Name:            "TestProject",
			IsTermsAccepted: true,
		})

		if err != nil {
			t.Fatal(err)
		}

		// "query {project(id:\"%s\"){id,name,members(offset:0, limit:50){user{firstName,lastName,email}},apiKeys{name,id,createdAt,projectID}}}"
		t.Run("Project query base info", func(t *testing.T) {
			query := fmt.Sprintf(
				"query {project(id:\"%s\"){id,name,description,createdAt}}",
				createdProject.ID.String(),
			)

			result := testQuery(t, query)

			data := result.(map[string]interface{})
			project := data[consoleql.ProjectQuery].(map[string]interface{})

			assert.Equal(t, createdProject.ID.String(), project[consoleql.FieldID])
			assert.Equal(t, createdProject.Name, project[consoleql.FieldName])
			assert.Equal(t, createdProject.Description, project[consoleql.FieldDescription])

			createdAt := time.Time{}
			err := createdAt.UnmarshalText([]byte(project[consoleql.FieldCreatedAt].(string)))

			assert.NoError(t, err)
			assert.Equal(t, createdProject.CreatedAt, createdAt)
		})

		user1, err := service.CreateUser(authCtx, console.CreateUser{
			UserInfo: console.UserInfo{
				FirstName: "Mickey",
				LastName:  "Last",
				Email:     "muu1@email.com",
			},
			Password: "123a123",
		})

		if err != nil {
			t.Fatal(err)
		}

		user2, err := service.CreateUser(authCtx, console.CreateUser{
			UserInfo: console.UserInfo{
				FirstName: "Dubas",
				LastName:  "Name",
				Email:     "muu2@email.com",
			},
			Password: "123a123",
		})

		if err != nil {
			t.Fatal(err)
		}

		err = service.AddProjectMembers(authCtx, createdProject.ID, []string{
			user1.Email,
			user2.Email,
		})

		if err != nil {
			t.Fatal(err)
		}

		t.Run("Project query team members", func(t *testing.T) {
			query := fmt.Sprintf(
				"query {project(id:\"%s\"){members(offset:0, limit:50){user{id,firstName,lastName,email,createdAt}}}}",
				createdProject.ID.String(),
			)

			result := testQuery(t, query)

			data := result.(map[string]interface{})
			project := data[consoleql.ProjectQuery].(map[string]interface{})
			members := project[consoleql.FieldMembers].([]interface{})

			assert.Equal(t, 3, len(members))

			testUser := func(t *testing.T, actual map[string]interface{}, expected *console.User) {
				assert.Equal(t, expected.Email, actual[consoleql.FieldEmail])
				assert.Equal(t, expected.FirstName, actual[consoleql.FieldFirstName])
				assert.Equal(t, expected.LastName, actual[consoleql.FieldLastName])

				createdAt := time.Time{}
				err := createdAt.UnmarshalText([]byte(actual[consoleql.FieldCreatedAt].(string)))

				assert.NoError(t, err)
				assert.Equal(t, expected.CreatedAt, createdAt)
			}

			var foundRoot, foundU1, foundU2 bool

			for _, entry := range members {
				member := entry.(map[string]interface{})
				user := member[consoleql.UserType].(map[string]interface{})

				id := user[consoleql.FieldID].(string)
				switch id {
				case rootUser.ID.String():
					foundRoot = true
					testUser(t, user, rootUser)
				case user1.ID.String():
					foundU1 = true
					testUser(t, user, user1)
				case user2.ID.String():
					foundU2 = true
					testUser(t, user, user2)
				}
			}

			assert.True(t, foundRoot)
			assert.True(t, foundU1)
			assert.True(t, foundU2)
		})

		keyInfo1, _, err := service.CreateAPIKey(authCtx, createdProject.ID, "key1")
		if err != nil {
			t.Fatal(err)
		}

		keyInfo2, _, err := service.CreateAPIKey(authCtx, createdProject.ID, "key2")
		if err != nil {
			t.Fatal(err)
		}

		t.Run("Project query api keys", func(t *testing.T) {
			query := fmt.Sprintf(
				"query {project(id:\"%s\"){apiKeys{name,id,createdAt,projectID}}}",
				createdProject.ID.String(),
			)

			result := testQuery(t, query)

			data := result.(map[string]interface{})
			project := data[consoleql.ProjectQuery].(map[string]interface{})
			keys := project[consoleql.FieldAPIKeys].([]interface{})

			assert.Equal(t, 2, len(keys))

			testAPIKey := func(t *testing.T, actual map[string]interface{}, expected *console.APIKeyInfo) {
				assert.Equal(t, expected.Name, actual[consoleql.FieldName])
				assert.Equal(t, expected.ProjectID.String(), actual[consoleql.FieldProjectID])

				createdAt := time.Time{}
				err := createdAt.UnmarshalText([]byte(actual[consoleql.FieldCreatedAt].(string)))

				assert.NoError(t, err)
				assert.Equal(t, expected.CreatedAt, createdAt)
			}

			var foundKey1, foundKey2 bool

			for _, entry := range keys {
				key := entry.(map[string]interface{})

				id := key[consoleql.FieldID].(string)
				switch id {
				case keyInfo1.ID.String():
					foundKey1 = true
					testAPIKey(t, key, keyInfo1)
				case keyInfo2.ID.String():
					foundKey2 = true
					testAPIKey(t, key, keyInfo2)
				}
			}

			assert.True(t, foundKey1)
			assert.True(t, foundKey2)
		})

		project2, err := service.CreateProject(authCtx, console.ProjectInfo{
			Name:            "Project2",
			Description:     "Test desc",
			IsTermsAccepted: true,
		})

		if err != nil {
			t.Fatal(err)
		}

		t.Run("MyProjects query", func(t *testing.T) {
			query := "query {myProjects{id,name,description,createdAt}}"

			result := testQuery(t, query)

			data := result.(map[string]interface{})
			projectsList := data[consoleql.MyProjectsQuery].([]interface{})

			assert.Equal(t, 2, len(projectsList))

			testProject := func(t *testing.T, actual map[string]interface{}, expected *console.Project) {
				assert.Equal(t, expected.Name, actual[consoleql.FieldName])
				assert.Equal(t, expected.Description, actual[consoleql.FieldDescription])

				createdAt := time.Time{}
				err := createdAt.UnmarshalText([]byte(actual[consoleql.FieldCreatedAt].(string)))

				assert.NoError(t, err)
				assert.Equal(t, expected.CreatedAt, createdAt)
			}

			var foundProj1, foundProj2 bool

			for _, entry := range projectsList {
				project := entry.(map[string]interface{})

				id := project[consoleql.FieldID].(string)
				switch id {
				case createdProject.ID.String():
					foundProj1 = true
					testProject(t, project, createdProject)
				case project2.ID.String():
					foundProj2 = true
					testProject(t, project, project2)
				}
			}

			assert.True(t, foundProj1)
			assert.True(t, foundProj2)
		})

		t.Run("Token query", func(t *testing.T) {
			query := fmt.Sprintf(
				"query {token(email: \"%s\", password: \"%s\"){token,user{id,email,firstName,lastName,createdAt}}}",
				createUser.Email,
				createUser.Password,
			)

			result := testQuery(t, query)

			data := result.(map[string]interface{})
			queryToken := data[consoleql.TokenQuery].(map[string]interface{})

			token := queryToken[consoleql.TokenType].(string)
			user := queryToken[consoleql.UserType].(map[string]interface{})

			tauth, err := service.Authorize(auth.WithAPIKey(ctx, []byte(token)))
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, rootUser.ID, tauth.User.ID)
			assert.Equal(t, rootUser.ID.String(), user[consoleql.FieldID])
			assert.Equal(t, rootUser.Email, user[consoleql.FieldEmail])
			assert.Equal(t, rootUser.FirstName, user[consoleql.FieldFirstName])
			assert.Equal(t, rootUser.LastName, user[consoleql.FieldLastName])

			createdAt := time.Time{}
			err = createdAt.UnmarshalText([]byte(user[consoleql.FieldCreatedAt].(string)))

			assert.NoError(t, err)
			assert.Equal(t, rootUser.CreatedAt, createdAt)
		})
	})
}
