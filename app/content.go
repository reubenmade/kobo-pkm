package main

import (
	"image"
	"image/color"
)

// Mock data — all of this becomes server-provided JSON later.

var mockEvents = []string{
	"09:30  Standup call",
	"12:00  Lunch with Anna",
	"15:00  Homeserver maintenance window",
	"19:30  Gym",
}

var mockTodos = []string{
	"Fix cleanup cron on listen-later",
	"Read chapter 4 of current book",
	"Order stylus nibs",
	"Water the plants",
}

var mockReads = []struct {
	Title string
	Pct   int
}{
	{"The Shallows — Nicholas Carr", 62},
	{"How Buildings Learn — Stewart Brand", 18},
}

var mockLoop = []struct{ Text, Src string }{
	{"We become, neurologically, what we think.", "The Shallows"},
	{"All buildings are predictions. All predictions are wrong.", "How Buildings Learn"},
	{"The best way to predict the future is to invent it.", "Alan Kay"},
}

type ArticleDef struct {
	ID       string
	Title    string
	Byline   string
	Paras    []string
	FigAfter map[int]int // paragraph index -> figure style
}

var lorem = []string{
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.",
	"Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Sed ut perspiciatis unde omnis iste natus error sit voluptatem accusantium doloremque laudantium, totam rem aperiam, eaque ipsa quae ab illo inventore veritatis et quasi architecto beatae vitae dicta sunt explicabo.",
	"Nemo enim ipsam voluptatem quia voluptas sit aspernatur aut odit aut fugit, sed quia consequuntur magni dolores eos qui ratione voluptatem sequi nesciunt. Neque porro quisquam est, qui dolorem ipsum quia dolor sit amet, consectetur, adipisci velit, sed quia non numquam eius modi tempora incidunt ut labore et dolore magnam aliquam quaerat voluptatem.",
	"Ut enim ad minima veniam, quis nostrum exercitationem ullam corporis suscipit laboriosam, nisi ut aliquid ex ea commodi consequatur. Quis autem vel eum iure reprehenderit qui in ea voluptate velit esse quam nihil molestiae consequatur, vel illum qui dolorem eum fugiat quo voluptas nulla pariatur.",
	"At vero eos et accusamus et iusto odio dignissimos ducimus qui blanditiis praesentium voluptatum deleniti atque corrupti quos dolores et quas molestias excepturi sint occaecati cupiditate non provident, similique sunt in culpa qui officia deserunt mollitia animi, id est laborum et dolorum fuga.",
	"Et harum quidem rerum facilis est et expedita distinctio. Nam libero tempore, cum soluta nobis est eligendi optio cumque nihil impedit quo minus id quod maxime placeat facere possimus, omnis voluptas assumenda est, omnis dolor repellendus.",
	"Temporibus autem quibusdam et aut officiis debitis aut rerum necessitatibus saepe eveniet ut et voluptates repudiandae sint et molestiae non recusandae. Itaque earum rerum hic tenetur a sapiente delectus, ut aut reiciendis voluptatibus maiores alias consequatur aut perferendis doloribus asperiores repellat.",
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit. Vivamus lacinia odio vitae vestibulum vestibulum. Cras venenatis euismod malesuada. Integer sit amet mi id sapien tempor molestie in nec massa. Fusce non ante sed lorem rutrum feugiat, et harum quidem rerum facilis est et expedita distinctio libero tempore soluta.",
}

var articles = []ArticleDef{
	{
		ID:     "art-001",
		Title:  "On the Care and Feeding of Ideas",
		Byline: "A. Placeholder — 12 min read",
		Paras:  lorem,
		FigAfter: map[int]int{
			1: 2, // colour test card after paragraph 2
			5: 1, // hatched grayscale figure after paragraph 6
		},
	},
	{
		ID:     "art-002",
		Title:  "Notes Toward a Slower Web",
		Byline: "B. Example — 6 min read",
		Paras:  lorem[:4],
	},
}

// drawFigure renders a procedural placeholder "image" (no assets needed).
func drawFigure(c *image.RGBA, r image.Rectangle, style int) {
	StrokeRect(c, r, 3, 0)
	in := r.Inset(20)
	switch style {
	case 0: // concentric circles + gradient bars
		cx, cy := in.Min.X+in.Dx()/3, in.Min.Y+in.Dy()/2
		for i, rad := 0, 40; i < 4; i, rad = i+1, rad+45 {
			Circle(c, cx, cy, rad, 6, uint8(i*60))
		}
		bx := in.Min.X + in.Dx()*3/5
		bw := (in.Max.X - bx) / 5
		for i := 0; i < 5; i++ {
			g := uint8(255 * i / 5)
			FillRect(c, image.Rect(bx+i*bw, in.Min.Y+20+i*18, bx+i*bw+bw-8, in.Max.Y-20), g)
		}
	case 2: // colour test card (Kaleido panel check)
		cols := []color.RGBA{
			{220, 30, 30, 255},  // red
			{30, 160, 40, 255},  // green
			{40, 60, 220, 255},  // blue
			{230, 190, 20, 255}, // yellow
			{200, 40, 180, 255}, // magenta
			{30, 190, 200, 255}, // cyan
		}
		bw := in.Dx() / len(cols)
		for i, col := range cols {
			FillRGB(c, image.Rect(in.Min.X+i*bw, in.Min.Y, in.Min.X+i*bw+bw-6, in.Min.Y+in.Dy()/2), col)
		}
		// colour circles on the lower half
		cy := in.Min.Y + in.Dy()*3/4
		for i, col := range cols {
			CircleRGB(c, in.Min.X+bw/2+i*bw, cy, 42, 10, col)
		}
	default: // hatched "photo" with horizon
		hy := in.Min.Y + in.Dy()*2/3
		for y := in.Min.Y; y < hy; y += 6 {
			HLine(c, in.Min.X, in.Max.X, y, 1, 160)
		}
		FillRect(c, image.Rect(in.Min.X, hy, in.Max.X, in.Max.Y), 90)
		Circle(c, in.Min.X+in.Dx()*3/4, in.Min.Y+in.Dy()/3, 50, 8, 0)
	}
	DrawStringTop(c, Small, "fig. — placeholder image", r.Min.X+10, r.Max.Y-40, 60)
}
