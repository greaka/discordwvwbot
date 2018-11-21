package main

// getKeyByValue is a helper function to get a key based on a value in a map[key]value
func getKeyByValue(a string, list map[string]string) string {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return ""
}

// getIndexByValue is a helper function to get an index based on a value in a map[index]value
func getWorldIDByName(a string, list map[int]*linkInfo) int {
	for i, b := range list {
		if b.Name == a {
			return i
		}
	}
	return -1
}

// indexOfString is a helper function to get an index based on a value in a [index]string
func indexOfString(a string, list []string) int {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return -1
}

// same as indexOf but for int
func indexOfInt(a int, list []int) int {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return -1
}

// remove is a helper function to remove an item from an array at an index. The order will not be kept!
func remove(array []string, index int) []string {
	array[len(array)-1], array[index] = array[index], array[len(array)-1]
	return array[:len(array)-1]
}
