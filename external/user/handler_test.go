package user_test

// func TestHandler_CreateUser(t *testing.T) {

// 	tests := []struct {
// 		name               string
// 		userService        *servicestubs.User
// 		request            *http.Request
// 		assertResponse     func(w *httptest.ResponseRecorder, t *testing.T)
// 		expectedStatusCode int
// 	}{
// 		{
// 			name:        "Failure -  Invalid request body",
// 			userService: &servicestubs.User{},
// 			request: httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(
// 				string(`{ "title": "Mr", "full_name": "John D Doe" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Check submitted user information."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name:        "Failure - Invalid Email",
// 			userService: &servicestubs.User{},
// 			request: httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(
// 				string(`{ "first_name": "john", "last_name": "doe", "email" : "johndoe.gmail.com" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Check submitted user information."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name:        "Failure - First name needed",
// 			userService: &servicestubs.User{},
// 			request: httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(
// 				string(`{ "first_name": "", "last_name": "doe", "email" : "johndoe@gmail.com" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Check submitted user information."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name:        "Failure - Last name needed",
// 			userService: &servicestubs.User{},
// 			request: httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(
// 				string(`{ "first_name": "john", "last_name": "", "email" : "johndoe@gmail.com" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Check submitted user information."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name: "Failure - User already exists",
// 			userService: &servicestubs.User{
// 				CreateUserError: errors.New(user.ErrKeyResourceConflict),
// 			},
// 			request: httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(
// 				string(`{ "first_name": "john", "last_name": "doe", "email" : "johndoe@gmail.com" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "User registered on system."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusConflict,
// 		},
// 		{
// 			name: "Failure - Service Error",
// 			userService: &servicestubs.User{
// 				CreateUserError: errors.New("UnknownServiceError"),
// 			},
// 			request: httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(
// 				string(`{ "first_name": "john", "last_name": "doe", "email" : "johndoe@gmail.com" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Internal Server Error"}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusInternalServerError,
// 		},
// 		{
// 			name: "Success",
// 			userService: &servicestubs.User{
// 				CreateUserResponse: &user.CreateUserResponse{
// 					User: *getMockCreatedUser(),
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodPost, "/v1/users", strings.NewReader(
// 				string(`{ "first_name": "john", "last_name": "doe", "email" : "johndoe@gmail.com" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := user.User{}

// 				res := response.DTO{
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("CreateUser() failed, cannot get res content: %v", err)
// 				}

// 				assert.Equal(t, user.User{
// 					ID:        "fcbd2a74-22ee-4b6f-8709-11772fce4afd",
// 					FirstName: "John",
// 					LastName:  "Doe",
// 					Email:     "johndoe@gmail.com",
// 					Roles:     []string{},
// 					Status:    "PROVISIONED",
// 					Verified: user.UserVerifcationStatus{
// 						EmailVerified: false,
// 					},
// 					Meta: user.UserMeta{
// 						CreatedAt: "2021-05-12T21:05:05",
// 					},
// 				}, embeddedResponse)
// 			},
// 			expectedStatusCode: http.StatusCreated,
// 		},
// 	}

// 	for _, test := range tests {
// 		t.Run(test.name, func(t *testing.T) {

// 			v := validator.NewValidator()
// 			w := httptest.NewRecorder()
// 			user.NewHandler(test.userService, v).CreateUser(w, test.request)

// 			assert.Equal(t, test.expectedStatusCode, w.Code)
// 			test.assertResponse(w, t)

// 		})
// 	}
// }

// func TestHandler_GetUserByID(t *testing.T) {
// 	tests := []struct {
// 		name               string
// 		userService        *servicestubs.User
// 		request            *http.Request
// 		assertResponse     func(w *httptest.ResponseRecorder, t *testing.T)
// 		expectedStatusCode int
// 		expectedMessage    string
// 	}{
// 		{
// 			name: "Success - User found",
// 			userService: &servicestubs.User{
// 				GetUserByIDResponse: &user.GetUserByIDResponse{
// 					User: getMockSampleUser()[0],
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/user/6ab2144b-692d-41e0-a4d3-9e811ed673b7", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := user.User{}

// 				res := response.DTO{
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("GetUserByID() failed, cannot get res content: %v", err)
// 				}

// 				expectedBody := user.User{
// 					ID:        "6ab2144b-692d-41e0-a4d3-9e811ed673b7",
// 					FirstName: "John",
// 					LastName:  "Doe",
// 					Email:     "johndoe@domain.com",
// 					Roles:     []string{},
// 					Status:    "PROVISIONED",
// 					Verified: user.UserVerifcationStatus{
// 						EmailVerified: false,
// 					},
// 					Meta: user.UserMeta{
// 						CreatedAt: "2021-02-11T11:09:33",
// 					},
// 				}

// 				assert.Equal(t, &expectedBody, res.Data)
// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 		{
// 			name: "Failure - User not found",
// 			userService: &servicestubs.User{
// 				GetUserByIDError: errors.New(user.ErrKeyResourceNotFound),
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/user/bd2cbad1-6ccf-48e3-bb92-bc9961bc011e", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "User resource not found."}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusNotFound,
// 		},
// 		{
// 			name:        "Failure - ID validation failure",
// 			userService: &servicestubs.User{},
// 			request:     httptest.NewRequest(http.MethodGet, "/user/incorrect-uuid-4", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. User ID missing or malformatted."}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 	}

// 	for _, test := range tests {
// 		t.Run(test.name, func(t *testing.T) {

// 			w := httptest.NewRecorder()
// 			v := validator.NewValidator()

// 			router := mux.NewRouter()

// 			router.HandleFunc("/user/{userID}", user.NewHandler(test.userService, v).GetUserByID)
// 			router.ServeHTTP(w, test.request)

// 			test.assertResponse(w, t)
// 			assert.Equal(t, test.expectedStatusCode, w.Code)

// 		})
// 	}
// }

// func TestHandler_UpdateUser(t *testing.T) {

// 	tests := []struct {
// 		name               string
// 		userService        *servicestubs.User
// 		request            *http.Request
// 		assertResponse     func(w *httptest.ResponseRecorder, t *testing.T)
// 		expectedStatusCode int
// 	}{
// 		{
// 			name:        "Failure -  Invalid request body",
// 			userService: &servicestubs.User{},
// 			request: httptest.NewRequest(http.MethodPatch, "/v1/users/6ab2144b-692d-41e0-a4d3-9e811ed673b7", strings.NewReader(
// 				string(`{ "title": "Mr", "full_name": "John D Doe" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Check submitted user information."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name:        "Failure - Name length too short",
// 			userService: &servicestubs.User{},
// 			request: httptest.NewRequest(http.MethodPatch, "/v1/users/6ab2144b-692d-41e0-a4d3-9e811ed673b7", strings.NewReader(
// 				string(`{ "first_name": "lee", "last_name": "p" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Check submitted user information."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name: "Failure - User not found",
// 			userService: &servicestubs.User{
// 				UpdateUserError: errors.New("UserResourceNotFound"),
// 			},
// 			request: httptest.NewRequest(http.MethodPatch, "/v1/users/021b68ff-eaf4-476a-87a0-01b5cf07fb31", strings.NewReader(
// 				string(`{ "first_name": "lee", "last_name": "anderson" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "User resource not found."}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusNotFound,
// 		},
// 		{
// 			name: "Failure - Service Error",
// 			userService: &servicestubs.User{
// 				UpdateUserError: errors.New("UnknownServiceError"),
// 			},
// 			request: httptest.NewRequest(http.MethodPatch, "/v1/users/6ab2144b-692d-41e0-a4d3-9e811ed673b7", strings.NewReader(
// 				string(`{ "first_name": "lee", "last_name": "anderson" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Internal Server Error"}, res.Status)
// 			},
// 			expectedStatusCode: http.StatusInternalServerError,
// 		},
// 		{
// 			name: "Success",
// 			userService: &servicestubs.User{
// 				UpdateUserResponse: &user.UpdateUserResponse{
// 					User: *getMockUpdatedUser(),
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodPatch, "/v1/users/6ab2144b-692d-41e0-a4d3-9e811ed673b7", strings.NewReader(
// 				string(`{ "first_name": "lee", "last_name": "anderson" }`))),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := user.User{}

// 				res := response.DTO{
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("UpdateUser() failed, cannot get res content: %v", err)
// 				}

// 				assert.Equal(t, user.User{
// 					ID:        "6ab2144b-692d-41e0-a4d3-9e811ed673b7",
// 					FirstName: "Lee",
// 					LastName:  "Anderson",
// 					Email:     "johndoe@domain.com",
// 					Roles:     []string{},
// 					Status:    "PROVISIONED",
// 					Verified: user.UserVerifcationStatus{
// 						EmailVerified: false,
// 					},
// 					Meta: user.UserMeta{
// 						CreatedAt: "2021-02-11T11:09:33",
// 						UpdatedAt: "2021-05-20T16:49:05",
// 					},
// 				}, embeddedResponse)
// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 	}

// 	for _, test := range tests {
// 		t.Run(test.name, func(t *testing.T) {

// 			w := httptest.NewRecorder()
// 			v := validator.NewValidator()

// 			router := mux.NewRouter()

// 			router.HandleFunc("/v1/users/{userID}", user.NewHandler(test.userService, v).UpdateUser)
// 			router.ServeHTTP(w, test.request)

// 			assert.Equal(t, test.expectedStatusCode, w.Code)
// 			test.assertResponse(w, t)

// 		})
// 	}
// }

// func TestHandler_GetUsers(t *testing.T) {

// 	expectedFullGetUsersBody := []user.User{
// 		{
// 			ID:        "6ab2144b-692d-41e0-a4d3-9e811ed673b7",
// 			FirstName: "John",
// 			LastName:  "Doe",
// 			Email:     "johndoe@domain.com",
// 			Roles:     []string{},
// 			Status:    "PROVISIONED",
// 			Verified: user.UserVerifcationStatus{
// 				EmailVerified: false,
// 			},
// 			Meta: user.UserMeta{
// 				CreatedAt: "2021-02-11T11:09:33",
// 			},
// 		},
// 		{
// 			ID:        "59e2fda6-2e86-4847-a186-6775bcfdecc1",
// 			FirstName: "Oliver",
// 			LastName:  "Abraham",
// 			Email:     "oliverabraham@domain.com",
// 			Roles:     []string{},
// 			Status:    "PROVISIONED",
// 			Verified: user.UserVerifcationStatus{
// 				EmailVerified: false,
// 			},
// 			Meta: user.UserMeta{
// 				CreatedAt: "2021-01-11T11:09:33",
// 			},
// 		},
// 		{
// 			ID:        "bf894231-1267-4f84-b186-a1232f043fe9",
// 			FirstName: "Phil",
// 			LastName:  "Anderson",
// 			Email:     "philanderson@domain.com",
// 			Roles:     []string{},
// 			Status:    "PROVISIONED",
// 			Verified: user.UserVerifcationStatus{
// 				EmailVerified: false,
// 			},
// 			Meta: user.UserMeta{
// 				CreatedAt: "2021-01-12T12:09:33",
// 			},
// 		},
// 		{
// 			ID:        "78c6d206-b4c7-4088-941b-afee13dc8fdc",
// 			FirstName: "Sam",
// 			LastName:  "Carr",
// 			Email:     "samcarr@domain.com",
// 			Roles: []string{
// 				"ADMIN",
// 			},
// 			Status: "ACTIVATE",
// 			Verified: user.UserVerifcationStatus{
// 				EmailVerified:   true,
// 				EmailVerifiedAt: "2021-01-12T12:11:33",
// 			},
// 			Meta: user.UserMeta{
// 				CreatedAt:       "2021-01-12T12:09:33",
// 				StatusChangedAt: "2021-01-12T12:12:33",
// 				ActivatedAt:     "2021-01-12T12:12:33",
// 			},
// 		},
// 		{
// 			ID:        "8e9db5f6-2ecb-4a86-befc-8a8117cfc403",
// 			FirstName: "Sean",
// 			LastName:  "Gill",
// 			Email:     "seangill@domain.com",
// 			Roles:     []string{},
// 			Status:    "PROVISIONED",
// 			Verified: user.UserVerifcationStatus{
// 				EmailVerified: false,
// 			},
// 			Meta: user.UserMeta{
// 				CreatedAt: "2021-01-12T17:00:33",
// 			},
// 		},
// 	}

// 	tests := []struct {
// 		name               string
// 		userService        *servicestubs.User
// 		request            *http.Request
// 		assertResponse     func(w *httptest.ResponseRecorder, t *testing.T)
// 		expectedStatusCode int
// 		expectedMessage    string
// 	}{
// 		{
// 			name: "Success - With Meta",
// 			userService: &servicestubs.User{
// 				GetUsersResponse: &user.GetUsersResponse{
// 					Total:        5,
// 					TotalPages:   1,
// 					UsersPerPage: 5,
// 					Page:         1,
// 					Users:        getMockUsers(),
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/users?order=created_at_asc&per_page=5&meta=true", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := []user.User{}

// 				metaResponse := make(map[string]interface{})

// 				res := response.DTO{
// 					Meta: metaResponse,
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("GetUsers() failed, cannot get res content: %v", err)
// 				}

// 				expectedBody := expectedFullGetUsersBody

// 				expectedMeta := map[string]interface{}{
// 					"users_per_page": float64(5),
// 					"total_users":    float64(5),
// 					"total_pages":    float64(1),
// 					"page":           float64(1),
// 				}

// 				assert.Equal(t, &expectedBody, res.Data)
// 				assert.Equal(t, expectedMeta, res.Meta)
// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 		{
// 			name: "Success - Without Meta",
// 			userService: &servicestubs.User{
// 				GetUsersResponse: &user.GetUsersResponse{
// 					Total:        4,
// 					TotalPages:   1,
// 					UsersPerPage: 4,
// 					Page:         1,
// 					Users:        getMockUsers(),
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/users?order=created_at_asc&per_page=4", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := []user.User{}

// 				metaResponse := make(map[string]interface{})

// 				res := response.DTO{
// 					Meta: metaResponse,
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("GetUsers() failed, cannot get res content: %v", err)
// 				}

// 				expectedBody := expectedFullGetUsersBody

// 				assert.Equal(t, &expectedBody, res.Data)
// 				assert.Empty(t, res.Meta)
// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 		{
// 			name: "Success - Random With Meta",
// 			userService: &servicestubs.User{
// 				GetUsersResponse: &user.GetUsersResponse{
// 					Total:        1,
// 					TotalPages:   1,
// 					UsersPerPage: 1,
// 					Page:         1,
// 					Users:        getMockSampleUser(),
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/users?rand=true&meta=true", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := []user.User{}

// 				metaResponse := make(map[string]interface{})

// 				res := response.DTO{
// 					Meta: metaResponse,
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("GetUsers() failed, cannot get res content: %v", err)
// 				}

// 				expectedBody := []user.User{
// 					{
// 						ID:        "6ab2144b-692d-41e0-a4d3-9e811ed673b7",
// 						FirstName: "John",
// 						LastName:  "Doe",
// 						Email:     "johndoe@domain.com",
// 						Roles:     []string{},
// 						Status:    "PROVISIONED",
// 						Verified: user.UserVerifcationStatus{
// 							EmailVerified: false,
// 						},
// 						Meta: user.UserMeta{
// 							CreatedAt: "2021-02-11T11:09:33",
// 						},
// 					}}
// 				expectedMeta := map[string]interface{}{
// 					"users_per_page": float64(1),
// 					"total_users":    float64(1),
// 					"total_pages":    float64(1),
// 					"page":           float64(1),
// 				}

// 				assert.Equal(t, &expectedBody, res.Data)
// 				assert.Equal(t, expectedMeta, res.Meta)
// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 		{
// 			name: "Success - Random No Meta",
// 			userService: &servicestubs.User{
// 				GetUsersResponse: &user.GetUsersResponse{
// 					Total:        1,
// 					TotalPages:   1,
// 					UsersPerPage: 1,
// 					Page:         1,
// 					Users:        getMockSampleUser(),
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/users?rand=true", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := []user.User{}

// 				metaResponse := make(map[string]interface{})

// 				res := response.DTO{
// 					Meta: metaResponse,
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("GetUsers() failed, cannot get res content: %v", err)
// 				}

// 				expectedBody := []user.User{
// 					{
// 						ID:        "6ab2144b-692d-41e0-a4d3-9e811ed673b7",
// 						FirstName: "John",
// 						LastName:  "Doe",
// 						Email:     "johndoe@domain.com",
// 						Roles:     []string{},
// 						Status:    "PROVISIONED",
// 						Verified: user.UserVerifcationStatus{
// 							EmailVerified: false,
// 						},
// 						Meta: user.UserMeta{
// 							CreatedAt: "2021-02-11T11:09:33",
// 						},
// 					}}

// 				assert.Equal(t, &expectedBody, res.Data)
// 				assert.Empty(t, res.Meta)
// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 		{
// 			name: "Success - No Users in repo",
// 			userService: &servicestubs.User{
// 				GetUsersResponse: &user.GetUsersResponse{
// 					Total:        0,
// 					TotalPages:   0,
// 					UsersPerPage: 0,
// 					Page:         0,
// 					Users:        []user.User{},
// 				},
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/users", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				embeddedResponse := []user.User{}

// 				res := response.DTO{
// 					Data: &embeddedResponse,
// 				}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("GetUsers() failed, cannot get res content: %v", err)
// 				}

// 				expectedBody := []user.User{}

// 				assert.Equal(t, &expectedBody, res.Data)
// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 		{
// 			name: "Failure - Unrecgnoised error",
// 			userService: &servicestubs.User{
// 				GetUsersError: errors.New("UnknownServiceError"),
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/users?order=created_at_asc&per_page=4", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Internal Server Error"}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusInternalServerError,
// 		},
// 		{
// 			name: "Failure - Page out of range",
// 			userService: &servicestubs.User{
// 				GetUsersError: errors.New("PageOutOfRange"),
// 			},
// 			request: httptest.NewRequest(http.MethodGet, "/users?order=created_at_asc&per_page=4", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Page out of range."}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name:        "Failure - Query param validation failure (Order)",
// 			userService: &servicestubs.User{},
// 			request:     httptest.NewRequest(http.MethodGet, "/users?order=invalid&per_page=4", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Invalid query param(s) passed."}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 		{
// 			name:        "Failure - Query param validation failure (status)",
// 			userService: &servicestubs.User{},
// 			request:     httptest.NewRequest(http.MethodGet, "/users?status=invalid&per_page=4", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. Invalid query param(s) passed."}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 	}

// 	for _, test := range tests {
// 		t.Run(test.name, func(t *testing.T) {

// 			w := httptest.NewRecorder()
// 			v := validator.NewValidator()

// 			user.NewHandler(test.userService, v).GetUsers(w, test.request)

// 			test.assertResponse(w, t)
// 			assert.Equal(t, test.expectedStatusCode, w.Code)

// 		})
// 	}
// }

// func TestHandler_DeleteUser(t *testing.T) {
// 	tests := []struct {
// 		name               string
// 		userService        *servicestubs.User
// 		request            *http.Request
// 		assertResponse     func(w *httptest.ResponseRecorder, t *testing.T)
// 		expectedStatusCode int
// 		expectedError      error
// 	}{
// 		{
// 			name: "Success - User deleted",
// 			userService: &servicestubs.User{
// 				DeleteUserError: nil,
// 			},
// 			request: httptest.NewRequest(http.MethodDelete, "/v1/users/6ab2144b-692d-41e0-a4d3-9e811ed673b7", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("UpdateUser() failed, cannot get res content: %v", err)
// 				}

// 				assert.Empty(t, res.Data)

// 			},
// 			expectedStatusCode: http.StatusOK,
// 		},
// 		{
// 			name: "Failure - User not found",
// 			userService: &servicestubs.User{
// 				DeleteUserError: errors.New("UserResourceNotFound"),
// 			},
// 			request: httptest.NewRequest(http.MethodDelete, "/v1/users/6ab2144b-692d-41e0-a4d3-9e811ed673b7", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "User resource not found."}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusNotFound,
// 		},
// 		{
// 			name: "Failure - Internal error",
// 			userService: &servicestubs.User{
// 				DeleteUserError: errors.New("boom boom pow"),
// 			},
// 			request: httptest.NewRequest(http.MethodDelete, "/v1/users/6ab2144b-692d-41e0-a4d3-9e811ed673b7", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Internal Server Error"}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusInternalServerError,
// 		},
// 		{
// 			name:        "Failure - ID validation failure",
// 			userService: &servicestubs.User{},
// 			request:     httptest.NewRequest(http.MethodDelete, "/v1/users/incorrect-uuid-4", nil),
// 			assertResponse: func(w *httptest.ResponseRecorder, t *testing.T) {

// 				res := response.DTO{}

// 				err := responsehelpers.UnmarshalResponseBody(w, &res)
// 				if err != nil {
// 					t.Fatalf("Cannot get response content: %v", err)
// 				}

// 				assert.Equal(t, &response.StatusDTO{Message: "Bad Request. User ID missing or malformatted."}, res.Status)

// 			},
// 			expectedStatusCode: http.StatusBadRequest,
// 		},
// 	}

// 	for _, test := range tests {
// 		t.Run(test.name, func(t *testing.T) {

// 			w := httptest.NewRecorder()
// 			v := validator.NewValidator()

// 			router := mux.NewRouter()

// 			router.HandleFunc("/v1/users/{userID}", user.NewHandler(test.userService, v).DeleteUser)
// 			router.ServeHTTP(w, test.request)

// 			test.assertResponse(w, t)
// 			assert.Equal(t, test.expectedStatusCode, w.Code)

// 		})
// 	}
// }

// func getMockUpdatedUser() *user.User {
// 	user := getMockUsers()[0]
// 	user.FirstName = "Lee"
// 	user.LastName = "Anderson"
// 	user.Meta.UpdatedAt = "2021-05-20T16:49:05"

// 	return &user

// }

// func getMockCreatedUser() *user.User {
// 	return &user.User{
// 		ID:        "fcbd2a74-22ee-4b6f-8709-11772fce4afd",
// 		FirstName: "John",
// 		LastName:  "Doe",
// 		Email:     "johndoe@gmail.com",
// 		Roles:     []string{},
// 		Status:    "PROVISIONED",
// 		Verified: user.UserVerifcationStatus{
// 			EmailVerified: false,
// 		},
// 		Meta: user.UserMeta{
// 			CreatedAt: "2021-05-12T21:05:05",
// 		},
// 	}
// }
