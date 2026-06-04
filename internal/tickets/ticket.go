// MIT License
//
// Copyright (c) 2026 QB Networks
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package tickets

import "time"

const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
)

type Ticket struct {
	ID          int       `json:"id"`
	Customer    string    `json:"customer"`
	Email       string    `json:"email"`
	Subject     string    `json:"subject"`
	Description string    `json:"description"`
	Priority    string    `json:"priority"`
	Status      string    `json:"status"`
	Assignees   []string  `json:"assignees"`
	Resolution  string    `json:"resolution,omitempty"`
	Comments    []Comment `json:"comments"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Comment struct {
	Author    string    `json:"author"`
	Message   string    `json:"message"`
	Internal  bool      `json:"internal"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateTicketRequest struct {
	Customer    string `json:"customer"`
	Email       string `json:"email"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

type UpdateTicketRequest struct {
	Status     string   `json:"status"`
	Assignees  []string `json:"assignees"`
	Resolution string   `json:"resolution"`
}

type AddCommentRequest struct {
	Author   string `json:"author"`
	Message  string `json:"message"`
	Internal bool   `json:"internal"`
}

type Stats struct {
	Open       int `json:"open"`
	InProgress int `json:"inProgress"`
	Resolved   int `json:"resolved"`
	Closed     int `json:"closed"`
	Total      int `json:"total"`
}
