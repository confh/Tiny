package vm

import (
	"reflect"
)

func OptimizeBytecode(instructions []Instruction) []Instruction {
	instructions = optimizeIncLocal(instructions)
	instructions = optimizeJumpLocalGEConst(instructions)
	instructions = optimizeAddAssignLocal(instructions)
	instructions = optimizeSubtractAssignLocal(instructions)
	instructions = optimizeJumpLocalGELocal(instructions)
	instructions = optimizeJumpModLocalLocalNotZero(instructions)
	instructions = optimizeJumpModLocalConstNotZero(instructions)
	instructions = optimizeArrayLocalMethodCalls(instructions)
	instructions = optimizeLocalMethodCalls(instructions)
	instructions = optimizeGetPropertyLocal(instructions)
	instructions = optimizeAddPropertyLocalLocal(instructions)
	instructions = optimizeMulLocalConst(instructions)
	instructions = optimizeLoadLocalSlots(instructions)
	instructions = optimizeJumpLocalGTConst(instructions)
	instructions = optimizeCallDirectSubConst(instructions)
	instructions = optimizeLoopCondition(instructions)
	instructions = optimizeAddLocalLocalStore(instructions)

	return instructions
}

func optimizeLoopCondition(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	getLocalSlot := func(inst Instruction) (int, bool) {
		switch inst.Op {
		case OP_LOAD_LOCAL_0:
			return 0, true
		case OP_LOAD_LOCAL_1:
			return 1, true
		case OP_LOAD_LOCAL_2:
			return 2, true
		case OP_LOAD_LOCAL_3:
			return 3, true
		case OP_LOAD_LOCAL:
			if slot, ok := inst.Value.(int); ok {
				return slot, true
			}
			if slot, ok := inst.Value.(int64); ok {
				return int(slot), true
			}
		}
		return 0, false
	}

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Look for: LOAD_LOCAL (i) -> LOAD_LOCAL (n) -> OP_LTE -> OP_JUMP_IF_FALSE
		if i+3 < len(instructions) &&
			instructions[i+2].Op == OP_LTE &&
			instructions[i+3].Op == OP_JUMP_IF_FALSE {

			slotA, okA := getLocalSlot(instructions[i])
			slotB, okB := getLocalSlot(instructions[i+1])
			target, okT := instructions[i+3].Value.(int)

			if okA && okB && okT {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_LOCAL_GT_LOCAL,
					Value: JumpLocalGTLocalInfo{
						SlotA:  slotA,
						SlotB:  slotB,
						Target: target,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex
				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)
	return optimized
}

func optimizeLoadLocalSlots(instructions []Instruction) []Instruction {
	for i := range instructions {
		if instructions[i].Op != OP_LOAD_LOCAL {
			continue
		}

		slot, ok := instructions[i].Value.(int)
		if !ok {
			continue
		}

		switch slot {
		case 0:
			instructions[i].Op = OP_LOAD_LOCAL_0
			instructions[i].Value = nil
		case 1:
			instructions[i].Op = OP_LOAD_LOCAL_1
			instructions[i].Value = nil
		case 2:
			instructions[i].Op = OP_LOAD_LOCAL_2
			instructions[i].Value = nil
		case 3:
			instructions[i].Op = OP_LOAD_LOCAL_3
			instructions[i].Value = nil
		}
	}

	return instructions
}

func optimizeAddPropertyLocalLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+4 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_GET_PROPERTY_LOCAL &&
			instructions[i+2].Op == OP_LOAD_LOCAL &&
			instructions[i+3].Op == OP_ADD &&
			instructions[i+4].Op == OP_SET_PROPERTY {

			objectSlot, objectOK := instructions[i].Value.(int)
			propertyInfo, propertyOK := instructions[i+1].Value.(PropertyLocalInfo)
			sourceSlot, sourceOK := instructions[i+2].Value.(int)
			setName, setOK := instructions[i+4].Value.(string)

			if objectOK && propertyOK && sourceOK && setOK &&
				propertyInfo.Slot == objectSlot && propertyInfo.Name == setName {
				optimized = append(optimized, Instruction{
					Op: OP_ADD_PROPERTY_LOCAL_LOCAL,
					Value: PropertyLocalAssignInfo{
						ObjectSlot: objectSlot,
						SourceSlot: sourceSlot,
						Name:       setName,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex
				oldToNew[i+4] = newIndex
				i += 5
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeMulLocalConst(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+2 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_CONST &&
			instructions[i+2].Op == OP_MUL {

			slot, slotOK := instructions[i].Value.(int)
			value, valueOK := constIntAmount(instructions[i+1].Value)

			if slotOK && valueOK {
				optimized = append(optimized, Instruction{
					Op: OP_MUL_LOCAL_CONST,
					Value: LocalConstInfo{
						Slot:  slot,
						Value: value,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				i += 3
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeGetPropertyLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+1 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_GET_PROPERTY {

			slot, slotOK := instructions[i].Value.(int)
			name, nameOK := instructions[i+1].Value.(string)

			if slotOK && nameOK {
				optimized = append(optimized, Instruction{
					Op: OP_GET_PROPERTY_LOCAL,
					Value: PropertyLocalInfo{
						Slot: slot,
						Name: name,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				i += 2
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeJumpLocalGTConst(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// OP_LOAD_LOCAL_0
		// OP_CONST
		// OP_LTE
		// OP_JUMP_IF_FALSE
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL_0 &&
			instructions[i+1].Op == OP_CONST &&
			instructions[i+2].Op == OP_LTE &&
			instructions[i+3].Op == OP_JUMP_IF_FALSE {

			var constValue int
			var constOK bool
			switch v := instructions[i+1].Value.(type) {
			case int:
				constValue = v
				constOK = true
			case int64:
				constValue = int(v)
				constOK = true
			}

			target, targetOK := instructions[i+3].Value.(int)

			if constOK && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_LOCAL_GT_CONST,
					Value: JumpLocalGTConstInfo{
						Slot:   0,
						Value:  constValue,
						Target: target,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeArrayLocalMethodCalls(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+1 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_METHOD_CALL {

			arraySlot, arrayOK := instructions[i].Value.(int)
			info, infoOK := instructions[i+1].Value.(MethodCallInfo)

			if arrayOK && infoOK && info.ArgCount == 0 && info.Method == "length" {
				optimized = append(optimized, Instruction{
					Op: OP_ARRAY_LEN_LOCAL,
					Value: ArrayLocalCallInfo{
						ArraySlot: arraySlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				i += 2
				continue
			}
		}

		if i+2 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_METHOD_CALL {

			arraySlot, arrayOK := instructions[i].Value.(int)
			argSlot, argOK := instructions[i+1].Value.(int)
			info, infoOK := instructions[i+2].Value.(MethodCallInfo)

			if arrayOK && argOK && infoOK && info.ArgCount == 1 {
				var op OpCode
				switch info.Method {
				case "get":
					op = OP_ARRAY_GET_LOCAL
				case "push":
					op = OP_ARRAY_PUSH_LOCAL
				default:
					op = 0
				}

				if op != 0 {
					optimized = append(optimized, Instruction{
						Op: op,
						Value: ArrayLocalCallInfo{
							ArraySlot: arraySlot,
							ArgSlot:   argSlot,
						},
						File:   instructions[i].File,
						Line:   instructions[i].Line,
						Column: instructions[i].Column,
					})

					oldToNew[i] = newIndex
					oldToNew[i+1] = newIndex
					oldToNew[i+2] = newIndex
					i += 3
					continue
				}
			}
		}

		if i+4 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_CONST &&
			instructions[i+3].Op == OP_MUL &&
			instructions[i+4].Op == OP_METHOD_CALL {

			arraySlot, arrayOK := instructions[i].Value.(int)
			argSlot, argOK := instructions[i+1].Value.(int)
			factor, factorOK := constIntAmount(instructions[i+2].Value)
			info, infoOK := instructions[i+4].Value.(MethodCallInfo)

			if arrayOK && argOK && factorOK && infoOK && info.Method == "push" && info.ArgCount == 1 {
				optimized = append(optimized, Instruction{
					Op: OP_ARRAY_PUSH_LOCAL_MUL_CONST,
					Value: ArrayLocalMulConstInfo{
						ArraySlot: arraySlot,
						ArgSlot:   argSlot,
						Factor:    factor,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex
				oldToNew[i+4] = newIndex
				i += 5
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeLocalMethodCalls(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+1 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_METHOD_CALL {

			receiverSlot, receiverOK := instructions[i].Value.(int)
			info, infoOK := instructions[i+1].Value.(MethodCallInfo)

			if receiverOK && infoOK && info.ArgCount == 0 {
				optimized = append(optimized, Instruction{
					Op: OP_METHOD_CALL_LOCAL_0,
					Value: MethodLocalCallInfo{
						Method:       info.Method,
						ReceiverSlot: receiverSlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				i += 2
				continue
			}
		}

		if i+2 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_METHOD_CALL {

			receiverSlot, receiverOK := instructions[i].Value.(int)
			argSlot, argOK := instructions[i+1].Value.(int)
			info, infoOK := instructions[i+2].Value.(MethodCallInfo)

			if receiverOK && argOK && infoOK && info.ArgCount == 1 {
				optimized = append(optimized, Instruction{
					Op: OP_METHOD_CALL_LOCAL_1,
					Value: MethodLocalCallInfo{
						Method:       info.Method,
						ReceiverSlot: receiverSlot,
						ArgSlot:      argSlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				i += 3
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeJumpModLocalConstNotZero(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+5 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_CONST &&
			instructions[i+2].Op == OP_MOD &&
			instructions[i+3].Op == OP_CONST &&
			instructions[i+4].Op == OP_EQ &&
			instructions[i+5].Op == OP_JUMP_IF_FALSE {

			leftSlot, leftOK := instructions[i].Value.(int)
			right, rightOK := constIntAmount(instructions[i+1].Value)
			zero, zeroOK := constIntAmount(instructions[i+3].Value)
			target, targetOK := instructions[i+5].Value.(int)

			if leftOK && rightOK && zeroOK && zero == 0 && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO,
					Value: JumpModLocalConstNotZeroInfo{
						LeftSlot: leftSlot,
						Right:    right,
						Target:   target,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex
				oldToNew[i+4] = newIndex
				oldToNew[i+5] = newIndex

				i += 6
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeJumpModLocalLocalNotZero(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL left
		// OP_LOAD_LOCAL right
		// OP_MOD
		// OP_CONST 0
		// OP_EQ
		// OP_JUMP_IF_FALSE target
		if i+5 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_MOD &&
			instructions[i+3].Op == OP_CONST &&
			instructions[i+4].Op == OP_EQ &&
			instructions[i+5].Op == OP_JUMP_IF_FALSE {

			leftSlot, leftOK := instructions[i].Value.(int)
			rightSlot, rightOK := instructions[i+1].Value.(int)
			zero, zeroOK := constIntAmount(instructions[i+3].Value)
			target, targetOK := instructions[i+5].Value.(int)

			if leftOK && rightOK && zeroOK && zero == 0 && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO,
					Value: JumpModLocalLocalNotZeroInfo{
						LeftSlot:  leftSlot,
						RightSlot: rightSlot,
						Target:    target,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex
				oldToNew[i+4] = newIndex
				oldToNew[i+5] = newIndex

				i += 6
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeAddAssignLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))

	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL target
		// OP_LOAD_LOCAL source
		// OP_ADD
		// OP_ASSIGN_LOCAL target
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_ADD &&
			instructions[i+3].Op == OP_ASSIGN_LOCAL {

			targetLoadSlot, targetLoadOK := instructions[i].Value.(int)
			sourceSlot, sourceOK := instructions[i+1].Value.(int)
			assignSlot, assignOK := instructions[i+3].Value.(int)

			if targetLoadOK && sourceOK && assignOK && targetLoadSlot == assignSlot {
				optimized = append(optimized, Instruction{
					Op: OP_ADD_ASSIGN_LOCAL,
					Value: AssignLocalInfo{
						TargetSlot: targetLoadSlot,
						SourceSlot: sourceSlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeSubtractAssignLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))

	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL target
		// OP_LOAD_LOCAL source
		// OP_ADD
		// OP_ASSIGN_LOCAL target
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_SUB &&
			instructions[i+3].Op == OP_ASSIGN_LOCAL {

			targetLoadSlot, targetLoadOK := instructions[i].Value.(int)
			sourceSlot, sourceOK := instructions[i+1].Value.(int)
			assignSlot, assignOK := instructions[i+3].Value.(int)

			if targetLoadOK && sourceOK && assignOK && targetLoadSlot == assignSlot {
				optimized = append(optimized, Instruction{
					Op: OP_SUB_ASSIGN_LOCAL,
					Value: AssignLocalInfo{
						TargetSlot: targetLoadSlot,
						SourceSlot: sourceSlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeJumpLocalGEConst(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))

	// old instruction index -> new instruction index
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Pattern:
		// OP_LOAD_LOCAL slot
		// OP_CONST int
		// OP_LT
		// OP_JUMP_IF_FALSE target
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_CONST &&
			instructions[i+2].Op == OP_LT &&
			instructions[i+3].Op == OP_JUMP_IF_FALSE {

			slot, slotOK := instructions[i].Value.(int)
			constValue, constOK := constIntAmount(instructions[i+1].Value)
			target, targetOK := instructions[i+3].Value.(int)

			if slotOK && constOK && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_LOCAL_GE_CONST,
					Value: JumpLocalGEConstInfo{
						Slot:   slot,
						Value:  constValue,
						Target: target, // old target for now, remapped below
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeJumpLocalGELocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_LOAD_LOCAL &&
			instructions[i+2].Op == OP_LT &&
			instructions[i+3].Op == OP_JUMP_IF_FALSE {

			leftSlot, leftOK := instructions[i].Value.(int)
			rightSlot, rightOK := instructions[i+1].Value.(int)
			target, targetOK := instructions[i+3].Value.(int)

			if leftOK && rightOK && targetOK {
				optimized = append(optimized, Instruction{
					Op: OP_JUMP_LOCAL_GE_LOCAL,
					Value: JumpLocalGELocalInfo{
						LeftSlot:  leftSlot,
						RightSlot: rightSlot,
						Target:    target,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeCallDirectSubConst(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		// Look for the sequence: LOAD_LOCAL_0 -> CONST -> SUB -> CALL_DIRECT
		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL_0 &&
			instructions[i+1].Op == OP_CONST &&
			instructions[i+2].Op == OP_SUB &&
			instructions[i+3].Op == OP_CALL_DIRECT {

			// Extract subtraction constant
			var subAmt int
			var constOK bool
			switch v := instructions[i+1].Value.(type) {
			case int:
				subAmt = v
				constOK = true
			case int64:
				subAmt = int(v)
				constOK = true
			}

			// Extract DirectCallInfo from the original OP_CALL_DIRECT
			callInfo, callOK := instructions[i+3].Value.(DirectCallInfo)

			if constOK && callOK {
				optimized = append(optimized, Instruction{
					Op: OP_CALL_DIRECT_SUB_CONST,
					Value: CallDirectSubConstInfo{
						Slot:     0, // Hardcoded to 0 since it's OP_LOAD_LOCAL_0
						SubValue: subAmt,
						FnID:     callInfo.ID,
						FnName:   callInfo.Name,
						ArgCount: callInfo.ArgCount,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func optimizeAddLocalLocalStore(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	getLocalSlot := func(inst Instruction) (int, bool) {
		switch inst.Op {
		case OP_LOAD_LOCAL_0:
			return 0, true
		case OP_LOAD_LOCAL_1:
			return 1, true
		case OP_LOAD_LOCAL_2:
			return 2, true
		case OP_LOAD_LOCAL_3:
			return 3, true
		case OP_LOAD_LOCAL:
			if slot, ok := inst.Value.(int); ok {
				return slot, true
			}
			if slot, ok := inst.Value.(int64); ok {
				return int(slot), true
			}
		}
		return 0, false
	}

	getAssignSlot := func(inst Instruction) (int, bool) {
		if inst.Op != OP_ASSIGN_LOCAL && inst.Op != OP_STORE_LOCAL {
			return 0, false
		}
		if slot, ok := inst.Value.(int); ok {
			return slot, true
		}
		if slot, ok := inst.Value.(int64); ok {
			return int(slot), true
		}
		v := reflect.ValueOf(inst.Value)
		if v.Kind() == reflect.Struct {

			f := v.FieldByName("Slot")
			if f.IsValid() && (f.Kind() == reflect.Int || f.Kind() == reflect.Int64) {
				return int(f.Int()), true
			}

			if v.NumField() >= 2 {
				f := v.Field(1)
				if f.Kind() == reflect.Int || f.Kind() == reflect.Int64 {
					return int(f.Int()), true
				}
			}
		}
		return 0, false
	}

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+3 < len(instructions) && instructions[i+2].Op == OP_ADD {
			slotA, okA := getLocalSlot(instructions[i])
			slotB, okB := getLocalSlot(instructions[i+1])
			destSlot, okD := getAssignSlot(instructions[i+3])

			if okA && okB && okD {
				optimized = append(optimized, Instruction{
					Op: OP_ADD_LOCAL_LOCAL_STORE,
					Value: AddLocalLocalStoreInfo{
						SlotA:    slotA,
						SlotB:    slotB,
						DestSlot: destSlot,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex
				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)
	remapJumpTargets(optimized, oldToNew)
	return optimized
}

func remapJumpTargets(instructions []Instruction, oldToNew []int) {
	for i := range instructions {
		switch instructions[i].Op {
		case OP_ADD_LOCAL_LOCAL_STORE:

		case OP_JUMP_LOCAL_GT_LOCAL:
			info := instructions[i].Value.(JumpLocalGTLocalInfo)
			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}
			instructions[i].Value = info
		case OP_CALL_DIRECT_SUB_CONST:

		case OP_JUMP_LOCAL_GT_CONST:
			info := instructions[i].Value.(JumpLocalGTConstInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info
		case OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO:
			info := instructions[i].Value.(JumpModLocalConstNotZeroInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info

		case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
			info := instructions[i].Value.(JumpModLocalLocalNotZeroInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info

		case OP_JUMP_LOCAL_GE_LOCAL:
			info := instructions[i].Value.(JumpLocalGELocalInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info

		case OP_JUMP, OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE:
			target, ok := instructions[i].Value.(int)
			if ok && target >= 0 && target < len(oldToNew) {
				newTarget := oldToNew[target]
				instructions[i].Value = newTarget
				instructions[i].IntArg = newTarget
			}

		case OP_JUMP_LOCAL_GE_CONST:
			info := instructions[i].Value.(JumpLocalGEConstInfo)

			if info.Target >= 0 && info.Target < len(oldToNew) {
				info.Target = oldToNew[info.Target]
			}

			instructions[i].Value = info

		case OP_SETUP_TRY:
			info, ok := instructions[i].Value.(TryInfo)
			if ok {
				if info.CatchIP >= 0 && info.CatchIP < len(oldToNew) {
					info.CatchIP = oldToNew[info.CatchIP]
				}

				instructions[i].Value = info
			}
		}
	}
}

func optimizeIncLocal(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	oldToNew := make([]int, len(instructions)+1)

	for i := 0; i < len(instructions); {
		newIndex := len(optimized)

		if i+3 < len(instructions) &&
			instructions[i].Op == OP_LOAD_LOCAL &&
			instructions[i+1].Op == OP_CONST &&
			(instructions[i+2].Op == OP_ADD || instructions[i+2].Op == OP_SUB) &&
			instructions[i+3].Op == OP_ASSIGN_LOCAL {

			loadSlot, loadOK := instructions[i].Value.(int)
			assignSlot, assignOK := instructions[i+3].Value.(int)

			amount, amountOK := constIntAmount(instructions[i+1].Value)

			if loadOK && assignOK && amountOK && loadSlot == assignSlot {
				if instructions[i+2].Op == OP_SUB {
					amount = -amount
				}

				optimized = append(optimized, Instruction{
					Op: OP_INC_LOCAL,
					Value: IncrementInfo{
						Slot:      loadSlot,
						IntAmount: amount,
						IsFloat:   false,
					},
					File:   instructions[i].File,
					Line:   instructions[i].Line,
					Column: instructions[i].Column,
				})

				oldToNew[i] = newIndex
				oldToNew[i+1] = newIndex
				oldToNew[i+2] = newIndex
				oldToNew[i+3] = newIndex

				i += 4
				continue
			}
		}

		optimized = append(optimized, instructions[i])
		oldToNew[i] = newIndex
		i++
	}

	oldToNew[len(instructions)] = len(optimized)

	remapJumpTargets(optimized, oldToNew)

	return optimized
}

func constIntAmount(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v == float64(int(v)) {
			return int(v), true
		}
		return 0, false
	case float32:
		if v == float32(int(v)) {
			return int(v), true
		}
		return 0, false
	default:
		return 0, false
	}
}
