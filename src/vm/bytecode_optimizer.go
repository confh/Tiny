package vm

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
	// instructions = removeDeadCodeAfterReturn(instructions)

	return instructions
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

func remapJumpTargets(instructions []Instruction, oldToNew []int) {
	for i := range instructions {
		switch instructions[i].Op {
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

		case OP_JUMP, OP_JUMP_IF_FALSE:
			target, ok := instructions[i].Value.(int)
			if ok && target >= 0 && target < len(oldToNew) {
				instructions[i].Value = oldToNew[target]
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

func removeDeadCodeAfterReturn(instructions []Instruction) []Instruction {
	optimized := make([]Instruction, 0, len(instructions))
	dead := false

	for _, instr := range instructions {
		if dead {
			// Stop removing when we hit a jump target-ish instruction.
			// For now, keep this conservative.
			if instr.Op == OP_SETUP_TRY {
				dead = false
			} else {
				continue
			}
		}

		optimized = append(optimized, instr)

		if instr.Op == OP_RETURN {
			dead = true
		}
	}

	return optimized
}
